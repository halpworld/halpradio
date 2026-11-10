package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Execution limits for guest code. wazero is configured with
// WithCloseOnContextDone, so these deadlines interrupt running guest code
// (including infinite loops) and close the module.
const (
	InitTimeout = 3 * time.Second // _start and on_init
	HookTimeout = 2 * time.Second // each event hook
	// CompileTimeout bounds compiling the guest module to machine code.
	// wazero checks it between functions (see compileGuest); a module is at
	// most maxWasmDownloadBytes, which bounds the work done by one function
	// and by decoding, neither of which can be interrupted.
	CompileTimeout = 10 * time.Second
	// eventQueueSize bounds the events waiting for one plugin; further
	// events are dropped while the plugin is busy.
	eventQueueSize = 16
	// flashPerSecond bounds ui_flash calls per plugin.
	flashPerSecond = 2
)

type hookCall struct {
	name    string
	payload []byte
}

// Sandbox manages the Wazero runtime and execution of an isolated Wasm plugin.
type Sandbox struct {
	mu           sync.Mutex
	manifest     Manifest
	state        PluginState
	approved     atomic.Bool
	running      atomic.Bool
	storage      *Storage
	runtime      wazero.Runtime
	compiledMod  wazero.CompiledModule
	modInstance  api.Module
	ctx          context.Context
	cancel       context.CancelFunc
	onNotify     func(title, msg string)
	onFlash      func(msg string)
	logHandler   func(level int, msg string)
	httpClient   *http.Client
	notifyMu     sync.Mutex
	notifyCount  int
	notifyWindow int64
	flashCount   int
	flashWindow  int64
	events       chan hookCall
	workerOnce   sync.Once
	errMu        sync.Mutex
	lastErr      string
}

// sanitizeString makes plugin-supplied text safe for the terminal and
// desktop notifications. It delegates to util.SanitizeDisplay (drops C0/C1
// controls, DEL, bidi and zero-width characters) and caps at maxLen runes.
func sanitizeString(s string, maxLen int) string {
	return util.SanitizeDisplay(s, maxLen)
}

func newSandboxedHTTPTransport(manifest Manifest) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   4 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	return &http.Transport{
		// No proxy: with a proxy the dial-time IP check below would run on the
		// proxy's address instead of the real destination.
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid network address %q: %w", addr, err)
			}

			// DNS Resolution with IP validation
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve host %q: %w", host, err)
			}
			if len(ips) == 0 {
				return nil, fmt.Errorf("no IP addresses resolved for %q", host)
			}

			var chosenIP net.IP
			for _, ip := range ips {
				if manifest.Permissions.IsIPAllowed(ip) {
					chosenIP = ip
					break
				}
			}

			if chosenIP == nil {
				return nil, fmt.Errorf("network access to resolved IP for %q denied by sandbox security policy", host)
			}

			targetAddr := net.JoinHostPort(chosenIP.String(), port)
			return dialer.DialContext(ctx, network, targetAddr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   4 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// NewSandbox instantiates a Wasm sandbox for the given plugin.
func NewSandbox(
	ctx context.Context,
	manifest Manifest,
	state PluginState,
	wasmBytes []byte,
	storage *Storage,
	onNotify func(title, msg string),
	onFlash func(msg string),
	logHandler func(level int, msg string),
) (*Sandbox, error) {
	// Bound every resource the module can make the host allocate (tables,
	// locals, section vectors) before wazero decodes it; see wasmcheck.go.
	checked, err := checkWasmModule(wasmBytes)
	if err != nil {
		return nil, fmt.Errorf("wasm module %s refused: %w", manifest.ID, err)
	}
	wasmBytes = checked

	sCtx, cancel := context.WithCancel(ctx)

	// Wazero runtime configuration with 32 MiB linear memory limit per sandbox.
	// WithCloseOnContextDone makes call deadlines actually interrupt guest
	// code: loops yield to Go and the module is closed when the context ends.
	rCfg := wazero.NewRuntimeConfig().WithMemoryLimitPages(maxWasmMemoryPages).WithCloseOnContextDone(true)
	r := wazero.NewRuntimeWithConfig(sCtx, rCfg)

	// Instantiate WASI preview 1
	if _, err := wasi_snapshot_preview1.Instantiate(sCtx, r); err != nil {
		r.Close(context.Background())
		cancel()
		return nil, fmt.Errorf("failed to set up WASI for %s: %w", manifest.ID, err)
	}

	sb := &Sandbox{
		manifest:   manifest,
		state:      state,
		storage:    storage,
		runtime:    r,
		ctx:        sCtx,
		cancel:     cancel,
		events:     make(chan hookCall, eventQueueSize),
		onNotify:   onNotify,
		onFlash:    onFlash,
		logHandler: logHandler,
		httpClient: &http.Client{
			Timeout:   5 * time.Second,
			Transport: newSandboxedHTTPTransport(manifest),
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				if !manifest.Permissions.CanAccessNetwork(req.URL.String()) {
					return fmt.Errorf("redirect to %s denied by plugin network policy", req.URL.String())
				}
				return nil
			},
		},
	}

	// Register host functions under "halpradio" module
	if err := sb.registerHostFunctions(sCtx, r); err != nil {
		r.Close(sCtx)
		cancel()
		return nil, fmt.Errorf("failed to register host module: %w", err)
	}

	// Compile the guest wasm module
	compiled, err := compileGuest(sCtx, r, wasmBytes, CompileTimeout)
	if err != nil {
		r.Close(context.Background())
		cancel()
		return nil, fmt.Errorf("failed to compile wasm module %s: %w", manifest.ID, err)
	}
	if len(compiled.ExportedMemories()) == 0 {
		r.Close(sCtx)
		cancel()
		return nil, fmt.Errorf("wasm module %s does not export a linear memory", manifest.ID)
	}
	sb.compiledMod = compiled
	sb.approved.Store(state.PermissionsApproved)

	return sb, nil
}

// compileGuest compiles a checked module under a deadline. wazero's
// compiler only looks at the context between functions, and only when it
// compiles with more than one worker, so two workers are requested; the
// deadline then stops compilation of a module with many functions. Decoding
// and the compilation of a single function are not interruptible; both are
// bounded by the module size cap (maxWasmDownloadBytes, 20 MiB). On
// platforms where wazero uses its interpreter the deadline has no effect
// (the interpreter compiles to an intermediate form, linear in input size).
func compileGuest(ctx context.Context, r wazero.Runtime, wasmBytes []byte, timeout time.Duration) (wazero.CompiledModule, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cctx = experimental.WithCompilationWorkers(cctx, 2)
	compiled, err := r.CompileModule(cctx, wasmBytes)
	if err != nil {
		if cerr := cctx.Err(); cerr != nil {
			return nil, fmt.Errorf("compilation stopped (%v): %w", cerr, err)
		}
		return nil, err
	}
	return compiled, nil
}

// displayName is the plugin's validated name, used to label its UI text so
// a plugin cannot pass its messages off as coming from halpradio itself.
func (s *Sandbox) displayName() string {
	if n := sanitizeString(s.manifest.Name, 40); n != "" {
		return n
	}
	return s.manifest.ID
}

func (s *Sandbox) setLastErr(msg string) {
	s.errMu.Lock()
	s.lastErr = msg
	s.errMu.Unlock()
}

// LastError returns the reason the sandbox stopped (timeout, trap, exit),
// or "" while it is healthy. It never blocks on a running guest call.
func (s *Sandbox) LastError() string {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.lastErr
}

// Running reports whether the guest module is instantiated and usable. It
// never blocks on a running guest call.
func (s *Sandbox) Running() bool {
	return s.running.Load()
}

func (s *Sandbox) log(level int, msg string) {
	if s.logHandler != nil {
		s.logHandler(level, fmt.Sprintf("[%s] %s", s.manifest.ID, msg))
	}
}

func (s *Sandbox) registerHostFunctions(ctx context.Context, r wazero.Runtime) error {
	builder := r.NewHostModuleBuilder("halpradio")

	// Host function: log(level, ptr, len)
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, level uint32, ptr uint32, length uint32) {
			if length == 0 || !s.approved.Load() {
				return
			}
			if length > 4096 {
				length = 4096
			}
			data, ok := mod.Memory().Read(ptr, length)
			if !ok {
				return
			}
			s.log(int(level), sanitizeString(string(data), 4096))
		}).
		Export("log")

	// Host function: ui_notify(title_ptr, title_len, msg_ptr, msg_len)
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, titlePtr uint32, titleLen uint32, msgPtr uint32, msgLen uint32) {
			if !s.approved.Load() {
				return
			}
			if titleLen > 256 {
				titleLen = 256
			}
			if msgLen > 1024 {
				msgLen = 1024
			}
			var title, message string
			if titleLen > 0 {
				if tBytes, ok := mod.Memory().Read(titlePtr, titleLen); ok {
					title = sanitizeString(string(tBytes), 256)
				}
			}
			if msgLen > 0 {
				if mBytes, ok := mod.Memory().Read(msgPtr, msgLen); ok {
					message = sanitizeString(string(mBytes), 1024)
				}
			}

			// Rate limiting (max 5 notifications per 10-second window)
			s.notifyMu.Lock()
			nowWindow := time.Now().Unix() / 10
			if s.notifyWindow != nowWindow {
				s.notifyWindow = nowWindow
				s.notifyCount = 0
			}
			s.notifyCount++
			allowNotify := s.notifyCount <= 5
			s.notifyMu.Unlock()

			if !allowNotify {
				s.log(2, "ui_notify rate limit exceeded (max 5 per 10s)")
				return
			}

			if s.onNotify != nil && (title != "" || message != "") {
				// Label the notification with the plugin's name so it cannot
				// pass itself off as a halpradio message.
				label := s.displayName()
				if title != "" {
					label += ": " + title
				}
				s.onNotify(label, message)
			}
		}).
		Export("ui_notify")

	// Host function: ui_flash(msg_ptr, msg_len)
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, msgPtr uint32, msgLen uint32) {
			if msgLen == 0 || !s.approved.Load() {
				return
			}
			if msgLen > 512 {
				msgLen = 512
			}

			// Rate limiting (max flashPerSecond per second); extra calls are dropped.
			s.notifyMu.Lock()
			nowWindow := time.Now().Unix()
			if s.flashWindow != nowWindow {
				s.flashWindow = nowWindow
				s.flashCount = 0
			}
			s.flashCount++
			allowFlash := s.flashCount <= flashPerSecond
			s.notifyMu.Unlock()
			if !allowFlash {
				return
			}

			if mBytes, ok := mod.Memory().Read(msgPtr, msgLen); ok {
				msg := sanitizeString(string(mBytes), 512)
				if s.onFlash != nil && msg != "" {
					s.onFlash("[" + s.displayName() + "] " + msg)
				}
			}
		}).
		Export("ui_flash")

	// Host function: storage_get(key_ptr, key_len, out_ptr, max_len) -> bytes written
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, keyPtr uint32, keyLen uint32, outPtr uint32, maxLen uint32) uint32 {
			if !s.approved.Load() || !s.manifest.Permissions.HasStorage() || s.storage == nil {
				s.log(2, "storage_get denied: storage permission not approved")
				return 0
			}
			if keyLen == 0 || keyLen > MaxKeyLength {
				return 0
			}
			keyBytes, ok := mod.Memory().Read(keyPtr, keyLen)
			if !ok {
				return 0
			}
			val, found := s.storage.Get(string(keyBytes))
			if !found {
				return 0
			}
			valBytes := []byte(val)
			if uint32(len(valBytes)) > maxLen {
				valBytes = valBytes[:maxLen]
			}
			if len(valBytes) > 0 {
				if !mod.Memory().Write(outPtr, valBytes) {
					return 0
				}
			}
			return uint32(len(valBytes))
		}).
		Export("storage_get")

	// Host function: storage_set(key_ptr, key_len, val_ptr, val_len) -> 0 for success, 1 for error
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, keyPtr uint32, keyLen uint32, valPtr uint32, valLen uint32) uint32 {
			if !s.approved.Load() || !s.manifest.Permissions.HasStorage() || s.storage == nil {
				s.log(2, "storage_set denied: storage permission not approved")
				return 1
			}
			if keyLen == 0 || keyLen > MaxKeyLength || valLen > MaxValueLength {
				return 1
			}
			keyBytes, ok := mod.Memory().Read(keyPtr, keyLen)
			if !ok {
				return 1
			}
			valBytes, ok := mod.Memory().Read(valPtr, valLen)
			if !ok {
				return 1
			}
			if err := s.storage.Set(string(keyBytes), string(valBytes)); err != nil {
				s.log(3, fmt.Sprintf("storage_set error: %v", err))
				return 1
			}
			return 0
		}).
		Export("storage_set")

	// Host function: http_fetch(url_ptr, url_len, method_ptr, method_len, body_ptr, body_len, out_ptr, max_len) -> bytes written
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, urlPtr uint32, urlLen uint32, methodPtr uint32, methodLen uint32, bodyPtr uint32, bodyLen uint32, outPtr uint32, maxLen uint32) uint32 {
			if urlLen == 0 || urlLen > 2048 {
				return 0
			}
			urlBytes, ok := mod.Memory().Read(urlPtr, urlLen)
			if !ok {
				return 0
			}
			targetURL := string(urlBytes)

			if !s.approved.Load() || !s.manifest.Permissions.CanAccessNetwork(targetURL) {
				s.log(2, fmt.Sprintf("http_fetch denied: target URL %q not permitted by sandbox policy", targetURL))
				return 0
			}

			method := "GET"
			if methodLen > 0 {
				if methodLen > 16 {
					return 0
				}
				if mBytes, ok := mod.Memory().Read(methodPtr, methodLen); ok {
					method = strings.ToUpper(strings.TrimSpace(string(mBytes)))
				}
			}

			// Validate allowed methods
			switch method {
			case "GET", "POST", "PUT", "DELETE", "HEAD", "PATCH":
			default:
				s.log(2, fmt.Sprintf("http_fetch denied: method %q not allowed", method))
				return 0
			}

			if bodyLen > 1024*1024 { // Max 1 MB request body
				s.log(2, "http_fetch denied: request body exceeds 1MB limit")
				return 0
			}

			var reqBody io.Reader
			if bodyLen > 0 {
				if bBytes, ok := mod.Memory().Read(bodyPtr, bodyLen); ok {
					reqBody = bytes.NewReader(bBytes)
				} else {
					return 0
				}
			}

			req, err := http.NewRequestWithContext(ctx, method, targetURL, reqBody)
			if err != nil {
				s.log(3, fmt.Sprintf("http_fetch request creation error: %v", err))
				return 0
			}
			req.Header.Set("User-Agent", "halpradio-plugin/"+s.manifest.ID)
			if bodyLen > 0 && req.Header.Get("Content-Type") == "" {
				req.Header.Set("Content-Type", "application/json")
			}

			resp, err := s.httpClient.Do(req)
			if err != nil {
				s.log(3, fmt.Sprintf("http_fetch error for %s: %v", targetURL, err))
				return 0
			}
			defer resp.Body.Close()

			if maxLen > 5*1024*1024 {
				maxLen = 5 * 1024 * 1024 // Max 5 MB response
			}

			respData, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxLen)))
			if err != nil {
				return 0
			}

			if len(respData) > 0 && maxLen > 0 {
				if !mod.Memory().Write(outPtr, respData) {
					return 0
				}
			}

			return uint32(len(respData))
		}).
		Export("http_fetch")

	_, err := builder.Instantiate(ctx)
	return err
}

// recoverGuestPanic converts a panic at the guest-call boundary into an
// error and tears the module down, so no plugin can crash the app.
func (s *Sandbox) recoverGuestPanic(where string, errp *error) {
	if r := recover(); r != nil {
		msg := fmt.Sprintf("plugin %s crashed in %s: %v", s.manifest.ID, where, r)
		s.setLastErr(msg)
		s.closeModuleLocked()
		*errp = errors.New(msg)
	}
}

func (s *Sandbox) closeModuleLocked() {
	s.running.Store(false)
	if s.modInstance != nil {
		_ = s.modInstance.Close(context.Background())
		s.modInstance = nil
	}
}

// Start instantiates the guest wasm module and runs initialization if available.
// It refuses to run any guest code for a plugin whose permissions are not
// approved. _start and on_init together are bounded by InitTimeout and
// HookTimeout.
func (s *Sandbox) Start(initCfg map[string]string) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.recoverGuestPanic("start", &err)

	if s.modInstance != nil {
		return nil // Already running
	}
	if !s.approved.Load() {
		return fmt.Errorf("plugin %s is not approved; refusing to run it", s.manifest.ID)
	}
	if s.ctx.Err() != nil {
		return fmt.Errorf("plugin %s sandbox is closed", s.manifest.ID)
	}

	cfg := wazero.NewModuleConfig().
		WithName(s.manifest.ID).
		WithStdin(bytes.NewReader(nil)).
		WithStdout(io.Discard).
		WithStderr(io.Discard)

	initCtx, initCancel := context.WithTimeout(s.ctx, InitTimeout)
	defer initCancel()

	mod, err := s.runtime.InstantiateModule(initCtx, s.compiledMod, cfg)
	if err != nil {
		if initCtx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("timed out after %s", InitTimeout)
		}
		s.setLastErr(fmt.Sprintf("failed to start: %v", err))
		return fmt.Errorf("failed to instantiate module %s: %w", s.manifest.ID, err)
	}
	s.modInstance = mod
	s.running.Store(true)

	// Invoke guest on_init hook if present
	if initCfg == nil {
		initCfg = make(map[string]string)
	}
	cfgData, _ := json.Marshal(initCfg)
	if hookErr := s.invokeHookLocked("on_init", cfgData); hookErr != nil && s.modInstance == nil {
		// The module was closed (timeout, trap that closed it, or exit).
		return fmt.Errorf("plugin %s failed in on_init: %w", s.manifest.ID, hookErr)
	}

	s.setLastErr("")
	s.workerOnce.Do(func() { go s.worker() })
	return nil
}

// worker delivers queued events one at a time until the sandbox is closed.
func (s *Sandbox) worker() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case ev := <-s.events:
			_ = s.InvokeHook(ev.name, ev.payload)
		}
	}
}

// Dispatch queues an event hook for asynchronous delivery and never blocks.
// When the plugin is busy and its queue is full, the event is dropped.
func (s *Sandbox) Dispatch(hookName string, payload []byte) bool {
	if !s.running.Load() || s.ctx.Err() != nil {
		return false
	}
	select {
	case s.events <- hookCall{name: hookName, payload: payload}:
		return true
	default:
		return false
	}
}

// Stop closes the module instance.
func (s *Sandbox) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeModuleLocked()
	return nil
}

// Close terminates runtime and context resources. Cancelling the context
// first interrupts any guest call in progress, so this does not wait on a
// looping plugin.
func (s *Sandbox) Close() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()

	s.closeModuleLocked()
	return s.runtime.Close(context.Background())
}

// UpdateState updates the active state (such as approved permissions).
// Revoking approval immediately gates every host function.
func (s *Sandbox) UpdateState(state PluginState) {
	s.approved.Store(state.PermissionsApproved)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

// InvokeHook calls an exported guest hook function with payload JSON safely within timeout.
func (s *Sandbox) InvokeHook(hookName string, payload []byte) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer s.recoverGuestPanic(hookName, &err)
	if !s.approved.Load() {
		return fmt.Errorf("plugin %s is not approved", s.manifest.ID)
	}
	return s.invokeHookLocked(hookName, payload)
}

// guestMemory returns the module's exported linear memory, or nil.
func (s *Sandbox) guestMemory() api.Memory {
	if mem := s.modInstance.ExportedMemory("memory"); mem != nil {
		return mem
	}
	for name := range s.compiledMod.ExportedMemories() {
		if mem := s.modInstance.ExportedMemory(name); mem != nil {
			return mem
		}
	}
	return nil
}

func (s *Sandbox) invokeHookLocked(hookName string, payload []byte) error {
	if s.modInstance == nil {
		return fmt.Errorf("plugin %s is not running", s.manifest.ID)
	}

	fn := s.modInstance.ExportedFunction(hookName)
	if fn == nil {
		return nil // Hook not implemented by guest, skip gracefully
	}

	mem := s.guestMemory()
	if mem == nil {
		return fmt.Errorf("plugin %s has no linear memory", s.manifest.ID)
	}

	ctx, cancel := context.WithTimeout(s.ctx, HookTimeout)
	defer cancel()

	var ptr uint32 = 0
	var length uint32 = uint32(len(payload))

	// If guest exports alloc/malloc, allocate buffer in guest memory
	allocFn := s.modInstance.ExportedFunction("alloc")
	if allocFn == nil {
		allocFn = s.modInstance.ExportedFunction("malloc")
	}

	var err error
	if allocFn != nil && length > 0 {
		var res []uint64
		res, err = allocFn.Call(ctx, uint64(length))
		if err == nil && len(res) > 0 {
			ptr = uint32(res[0])
			if !mem.Write(ptr, payload) {
				return fmt.Errorf("failed to write payload to guest memory")
			}
		}
	} else if length > 0 {
		if mem.Size() >= length {
			_ = mem.Write(0, payload)
			ptr = 0
		}
	}

	// Call exported hook: fn(ptr, len)
	if err == nil {
		_, err = fn.Call(ctx, uint64(ptr), uint64(length))
	}

	// Clean up guest memory if free/dealloc is exported
	if allocFn != nil && ptr != 0 && !s.modInstance.IsClosed() {
		freeFn := s.modInstance.ExportedFunction("free")
		if freeFn == nil {
			freeFn = s.modInstance.ExportedFunction("dealloc")
		}
		if freeFn != nil {
			_, _ = freeFn.Call(ctx, uint64(ptr), uint64(length))
		}
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			err = fmt.Errorf("timed out after %s", HookTimeout)
		}
		s.log(3, fmt.Sprintf("hook %s failed: %v", hookName, err))
		if s.modInstance.IsClosed() {
			// Deadline, cancellation or proc_exit closed the module: it
			// cannot be used again. Record why and stop.
			s.setLastErr(fmt.Sprintf("stopped in %s: %v", hookName, err))
			s.closeModuleLocked()
		}
		return err
	}

	return nil
}
