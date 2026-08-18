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
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// Sandbox manages the Wazero runtime and execution of an isolated Wasm plugin.
type Sandbox struct {
	mu           sync.Mutex
	manifest     Manifest
	state        PluginState
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
}

// sanitizeString removes ASCII control characters and enforces a maximum byte length.
func sanitizeString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	res := b.String()
	if len(res) > maxLen {
		res = res[:maxLen]
	}
	return res
}

func newSandboxedHTTPTransport(manifest Manifest) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   4 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
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
	sCtx, cancel := context.WithCancel(ctx)

	// Wazero runtime configuration with 32 MB linear memory limit per sandbox
	rCfg := wazero.NewRuntimeConfig().WithMemoryLimitPages(512)
	r := wazero.NewRuntimeWithConfig(sCtx, rCfg)

	// Instantiate WASI preview 1
	wasi_snapshot_preview1.MustInstantiate(sCtx, r)

	sb := &Sandbox{
		manifest:   manifest,
		state:      state,
		storage:    storage,
		runtime:    r,
		ctx:        sCtx,
		cancel:     cancel,
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
	compiled, err := r.CompileModule(sCtx, wasmBytes)
	if err != nil {
		r.Close(sCtx)
		cancel()
		return nil, fmt.Errorf("failed to compile wasm module %s: %w", manifest.ID, err)
	}
	sb.compiledMod = compiled

	return sb, nil
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
			if length == 0 {
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
				s.onNotify(title, message)
			}
		}).
		Export("ui_notify")

	// Host function: ui_flash(msg_ptr, msg_len)
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, msgPtr uint32, msgLen uint32) {
			if msgLen == 0 {
				return
			}
			if msgLen > 512 {
				msgLen = 512
			}
			if mBytes, ok := mod.Memory().Read(msgPtr, msgLen); ok {
				if s.onFlash != nil {
					s.onFlash(sanitizeString(string(mBytes), 512))
				}
			}
		}).
		Export("ui_flash")

	// Host function: storage_get(key_ptr, key_len, out_ptr, max_len) -> bytes written
	builder.NewFunctionBuilder().
		WithFunc(func(ctx context.Context, mod api.Module, keyPtr uint32, keyLen uint32, outPtr uint32, maxLen uint32) uint32 {
			if !s.state.PermissionsApproved || !s.manifest.Permissions.HasStorage() || s.storage == nil {
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
			if !s.state.PermissionsApproved || !s.manifest.Permissions.HasStorage() || s.storage == nil {
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

			if !s.state.PermissionsApproved || !s.manifest.Permissions.CanAccessNetwork(targetURL) {
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

// Start instantiates the guest wasm module and runs initialization if available.
func (s *Sandbox) Start(initCfg map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.modInstance != nil {
		return nil // Already running
	}

	cfg := wazero.NewModuleConfig().
		WithName(s.manifest.ID).
		WithStdin(bytes.NewReader(nil)).
		WithStdout(io.Discard).
		WithStderr(io.Discard)

	initCtx, initCancel := context.WithTimeout(s.ctx, 3*time.Second)
	defer initCancel()

	mod, err := s.runtime.InstantiateModule(initCtx, s.compiledMod, cfg)
	if err != nil {
		return fmt.Errorf("failed to instantiate module %s: %w", s.manifest.ID, err)
	}
	s.modInstance = mod

	// Invoke guest on_init hook if present
	if initCfg == nil {
		initCfg = make(map[string]string)
	}
	cfgData, _ := json.Marshal(initCfg)
	_ = s.invokeHookLocked("on_init", cfgData)

	return nil
}

// Stop closes the module instance.
func (s *Sandbox) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.modInstance != nil {
		_ = s.modInstance.Close(s.ctx)
		s.modInstance = nil
	}
	return nil
}

// Close terminates runtime and context resources.
func (s *Sandbox) Close() error {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.modInstance != nil {
		_ = s.modInstance.Close(s.ctx)
		s.modInstance = nil
	}
	return s.runtime.Close(s.ctx)
}

// UpdateState updates the active state (such as approved permissions).
func (s *Sandbox) UpdateState(state PluginState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
}

// InvokeHook calls an exported guest hook function with payload JSON safely within timeout.
func (s *Sandbox) InvokeHook(hookName string, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.invokeHookLocked(hookName, payload)
}

func (s *Sandbox) invokeHookLocked(hookName string, payload []byte) error {
	if s.modInstance == nil {
		return fmt.Errorf("plugin %s is not running", s.manifest.ID)
	}

	fn := s.modInstance.ExportedFunction(hookName)
	if fn == nil {
		return nil // Hook not implemented by guest, skip gracefully
	}

	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()

	var ptr uint32 = 0
	var length uint32 = uint32(len(payload))

	// If guest exports alloc/malloc, allocate buffer in guest memory
	allocFn := s.modInstance.ExportedFunction("alloc")
	if allocFn == nil {
		allocFn = s.modInstance.ExportedFunction("malloc")
	}

	if allocFn != nil && length > 0 {
		res, err := allocFn.Call(ctx, uint64(length))
		if err == nil && len(res) > 0 {
			ptr = uint32(res[0])
			if !s.modInstance.Memory().Write(ptr, payload) {
				return fmt.Errorf("failed to write payload to guest memory")
			}
		}
	} else if length > 0 {
		mem := s.modInstance.Memory()
		if mem != nil && mem.Size() >= length {
			_ = mem.Write(0, payload)
			ptr = 0
		}
	}

	// Call exported hook: fn(ptr, len)
	_, err := fn.Call(ctx, uint64(ptr), uint64(length))

	// Clean up guest memory if free/dealloc is exported
	if allocFn != nil && ptr != 0 {
		freeFn := s.modInstance.ExportedFunction("free")
		if freeFn == nil {
			freeFn = s.modInstance.ExportedFunction("dealloc")
		}
		if freeFn != nil {
			_, _ = freeFn.Call(ctx, uint64(ptr), uint64(length))
		}
	}

	if err != nil {
		s.log(3, fmt.Sprintf("hook %s failed: %v", hookName, err))
		return err
	}

	return nil
}
