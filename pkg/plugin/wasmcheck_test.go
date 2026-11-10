package plugin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
)

func wsec(id byte, body ...byte) []byte {
	return cat([]byte{id}, appendULEB(nil, uint64(len(body))), body)
}

// (memory (export "memory") 1), shared by the fixtures below.
var (
	memSec    = wsec(wasmSectionMemory, 0x01, 0x00, 0x01)
	memExport = wsec(wasmSectionExport, 0x01, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00)
)

// RR-C-4 reviewer module: (table 134217728 funcref) (memory (export "memory") 1).
// Before the fix, Start allocated ~1 GiB of host heap for the table.
var wasmBigTable = cat(hdr,
	[]byte{0x04, 0x07, 0x01, 0x70, 0x00, 0x80, 0x80, 0x80, 0x40},
	memSec, memExport,
)

// One function declaring 2^32-1 locals: wazero would make([]ValueType, 0, 4G).
var wasmLocalsBomb = cat(hdr,
	wsec(wasmSectionType, 0x01, 0x60, 0x00, 0x00),
	wsec(wasmSectionFunction, 0x01, 0x00),
	memSec, memExport,
	wsec(wasmSectionCode, 0x01, 0x08, 0x01, 0xff, 0xff, 0xff, 0xff, 0x0f, 0x7f, 0x0b),
)

// (table 1 funcref) (func (export "grow") (result i32)
//
//	ref.null func  i32.const 1000000  table.grow 0)
var wasmTableGrow = cat(hdr,
	wsec(wasmSectionType, 0x01, 0x60, 0x00, 0x01, 0x7f),
	wsec(wasmSectionFunction, 0x01, 0x00),
	wsec(wasmSectionTable, 0x01, 0x70, 0x00, 0x01),
	memSec,
	wsec(wasmSectionExport, 0x02,
		0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
		0x04, 'g', 'r', 'o', 'w', 0x00, 0x00),
	wsec(wasmSectionCode, 0x01, 0x0b,
		0x00, 0xd0, 0x70, 0x41, 0xc0, 0x84, 0x3d, 0xfc, 0x0f, 0x00, 0x0b),
)

func heapDuring(t *testing.T, f func()) uint64 {
	t.Helper()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestOversizedTableRejectedWithoutAllocating(t *testing.T) {
	m := Manifest{ID: "big", Name: "Big", Version: "1.0.0", WasmFile: "plugin.wasm"}
	var err error
	used := heapDuring(t, func() {
		var sb *Sandbox
		sb, err = NewSandbox(context.Background(), m, approvedState(), wasmBigTable, nil, nil, nil, nil)
		if err == nil {
			err = sb.Start(nil)
			sb.Close()
		}
	})
	if err == nil || !strings.Contains(err.Error(), "table 0: minimum 134217728 entries exceeds limit") {
		t.Fatalf("want table-limit error, got %v", err)
	}
	if used > 8<<20 {
		t.Fatalf("host allocated %d MiB while refusing the module", used>>20)
	}
}

func TestResourceBombsRejected(t *testing.T) {
	cases := []struct {
		name string
		wasm []byte
		want string
	}{
		{"locals", wasmLocalsBomb, "more than 32768 locals"},
		{"element count", cat(hdr, memSec, memExport,
			wsec(wasmSectionElement, 0xff, 0xff, 0xff, 0xff, 0x0f)), "element segment count"},
		{"data length", cat(hdr, memSec, memExport,
			wsec(wasmSectionData, 0x01, 0x01, 0x80, 0x80, 0x40)), "larger than the section"},
		{"data length over file cap", cat(hdr, memSec, memExport,
			wsec(wasmSectionData, 0x01, 0x01, 0xff, 0xff, 0xff, 0xff, 0x0f)), "exceeds limit"},
		{"type count", cat(hdr, wsec(wasmSectionType, 0xff, 0xff, 0xff, 0x0f)), "type count"},
		{"export count backed by no bytes", cat(hdr, wsec(wasmSectionExport, 0x80, 0x40)), "larger than the section"},
		{"too many tables", cat(hdr, wsec(wasmSectionTable, 0x09)), "table count 9 exceeds limit 8"},
		{"memory min over cap", cat(hdr, wsec(wasmSectionMemory, 0x01, 0x00, 0xd8, 0x04), memExport), "minimum 600 pages"},
		{"memory max over cap", cat(hdr, wsec(wasmSectionMemory, 0x01, 0x01, 0x01, 0xe8, 0x07), memExport), "maximum 1000 pages"},
		{"two memories", cat(hdr, wsec(wasmSectionMemory, 0x02, 0x00, 0x01, 0x00, 0x01)), "memory count 2"},
		{"shared memory", cat(hdr, wsec(wasmSectionMemory, 0x01, 0x03, 0x01, 0x01)), "unsupported limits flag"},
		{"table import", cat(hdr, wsec(wasmSectionImport, 0x01,
			0x03, 'e', 'n', 'v', 0x01, 't', 0x01, 0x70, 0x00, 0x80, 0x80, 0x80, 0x40)), "only function imports"},
		{"duplicate section", cat(hdr, memSec, memSec), "duplicate section"},
		{"truncated section", cat(hdr, []byte{wasmSectionType, 0x7f, 0x01}), "larger than the file"},
		{"bad magic", []byte("\x00asx\x01\x00\x00\x00"), "not a WebAssembly binary"},
	}
	m := Manifest{ID: "bomb", Name: "Bomb", Version: "1.0.0", WasmFile: "plugin.wasm"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			used := heapDuring(t, func() {
				_, err = NewSandbox(context.Background(), m, approvedState(), tc.wasm, nil, nil, nil, nil)
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
			if used > 8<<20 {
				t.Fatalf("host allocated %d MiB while refusing the module", used>>20)
			}
		})
	}
}

// A table without a declared maximum is clamped, so table.grow beyond the
// limit fails inside the guest (-1) instead of allocating host memory.
func TestTableGrowBounded(t *testing.T) {
	checked, err := checkWasmModule(wasmTableGrow)
	if err != nil {
		t.Fatalf("checkWasmModule: %v", err)
	}
	ctx := context.Background()
	r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(maxWasmMemoryPages))
	defer r.Close(ctx)

	// Control: the unmodified module grows by 1,000,000 entries.
	raw, err := r.Instantiate(ctx, wasmTableGrow)
	if err != nil {
		t.Fatalf("instantiate raw: %v", err)
	}
	res, err := raw.ExportedFunction("grow").Call(ctx)
	if err != nil || int32(res[0]) != 1 {
		t.Fatalf("control: grow = %v, %v; want old size 1", res, err)
	}

	mod, err := r.InstantiateWithConfig(ctx, checked, wazero.NewModuleConfig().WithName("checked"))
	if err != nil {
		t.Fatalf("instantiate checked: %v", err)
	}
	res, err = mod.ExportedFunction("grow").Call(ctx)
	if err != nil {
		t.Fatalf("grow: %v", err)
	}
	if int32(res[0]) != -1 {
		t.Fatalf("table.grow past %d entries returned %d, want -1", maxTableElements, int32(res[0]))
	}
}

func TestCustomSectionsDropped(t *testing.T) {
	// A "name" custom section with an absurd count must not reach wazero.
	custom := wsec(wasmSectionCustom, 0x04, 'n', 'a', 'm', 'e', 0x01, 0xff, 0xff, 0xff, 0xff, 0x0f)
	in := cat(hdr, custom, memSec, memExport)
	out, err := checkWasmModule(in)
	if err != nil {
		t.Fatalf("checkWasmModule: %v", err)
	}
	if want := cat(hdr, memSec, memExport); string(out) != string(want) {
		t.Fatalf("custom section not dropped: % x", out)
	}
}

func TestHandAssembledFixturesStillLoad(t *testing.T) {
	for name, w := range map[string][]byte{
		"benign": wasmBenign, "loopStart": wasmLoopStart, "loopHook": wasmLoopHook,
		"flash": wasmFlash, "tableGrow": wasmTableGrow,
	} {
		if _, err := checkWasmModule(w); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The checker must accept what the documented Go toolchain produces.
func TestGoToolchainPluginLoads(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a wasip1 binary")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not on PATH")
	}
	dir := t.TempDir()
	src := `package main

import (
	"fmt"
	"sort"
	"strings"
)

func main() {
	s := []string{"b", "a", "c"}
	sort.Strings(s)
	fmt.Println(strings.Join(s, ","))
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module p\n\ngo 1.21\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "plugin.wasm")
	cmd := exec.Command(goBin, "build", "-ldflags=-s -w", "-o", out, ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm", "GOFLAGS=-mod=mod", "GOWORK=off")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cannot build wasip1 binary here: %v\n%s", err, b)
	}
	wasm, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	m := Manifest{ID: "gotool", Name: "Go", Version: "1.0.0", WasmFile: "plugin.wasm"}
	sb, err := NewSandbox(context.Background(), m, approvedState(), wasm, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Go toolchain output refused: %v", err)
	}
	sb.Close()
}

// FR-C-4 reviewer module: an expression element segment whose reference
// type is the typed-reference prefix 0x64. wazero reads 0x64 plus a
// heap-type LEB, the old checker read one byte, and the two parsers then
// disagreed on the vector length (wazero allocated 768 MiB for 147 bytes).
func elemDesyncModule(countTail ...byte) []byte {
	body := []byte{0x01, 0x05, 0x64, 0x70, 0xd2}
	body = append(body, countTail...)
	body = append(body, 0x0b)
	body = append(body, bytes.Repeat([]byte{0x0b}, 111)...)
	return cat(hdr, memSec, memExport, wsec(wasmSectionElement, body...))
}

func TestElementReftypeDesyncRefused(t *testing.T) {
	for _, w := range [][]byte{elemDesyncModule(0x80, 0x80, 0x10), elemDesyncModule(0xff, 0xff, 0xff, 0x0f)} {
		if _, err := checkWasmModule(w); err == nil || !strings.Contains(err.Error(), "unsupported reference type 0x64") {
			t.Fatalf("checker accepted desync module (%d bytes): %v", len(w), err)
		}
	}
	m := Manifest{ID: "elem", Name: "Elem", Version: "1.0.0", WasmFile: "plugin.wasm"}
	var err error
	used := heapDuring(t, func() {
		_, err = NewSandbox(context.Background(), m, approvedState(), elemDesyncModule(0x80, 0x80, 0x10), nil, nil, nil, nil)
	})
	if err == nil {
		t.Fatal("NewSandbox accepted the desync module")
	}
	if used > 8<<20 {
		t.Fatalf("host allocated %d MiB while refusing the module", used>>20)
	}
}

func TestElementKindAndRefTypeExact(t *testing.T) {
	seg := func(b ...byte) []byte {
		return cat(hdr, memSec, memExport, wsec(wasmSectionElement, append([]byte{0x01}, b...)...))
	}
	cases := []struct {
		name string
		wasm []byte
		ok   bool
	}{
		{"passive index elemkind 0x00", seg(0x01, 0x00, 0x00), true},
		{"passive index elemkind 0x01", seg(0x01, 0x01, 0x00), false},
		{"passive index elemkind 0x70", seg(0x01, 0x70, 0x00), false},
		{"declarative index elemkind 0x00", seg(0x03, 0x00, 0x00), true},
		{"declarative index elemkind 0x63", seg(0x03, 0x63, 0x70, 0x00), false},
		{"passive expr funcref", seg(0x05, 0x70, 0x00), true},
		{"passive expr externref", seg(0x07, 0x6f, 0x00), true},
		{"passive expr 0x63 prefix", seg(0x05, 0x63, 0x70, 0x00), false},
		{"passive expr 0x64 prefix", seg(0x05, 0x64, 0x70, 0x00), false},
		{"passive expr i32", seg(0x05, 0x7f, 0x00), false},
		{"flags 8", seg(0x08, 0x00, 0x00), false},
	}
	for _, tc := range cases {
		_, err := checkWasmModule(tc.wasm)
		if (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// manyFuncsModule returns a module with n empty functions.
func manyFuncsModule(n int) []byte {
	fn := appendULEB(nil, uint64(n))
	code := appendULEB(nil, uint64(n))
	for i := 0; i < n; i++ {
		fn = append(fn, 0x00)
		code = append(code, 0x02, 0x00, 0x0b)
	}
	return cat(hdr,
		wsec(wasmSectionType, 0x01, 0x60, 0x00, 0x00),
		wsec(wasmSectionFunction, fn...),
		memSec, memExport,
		wsec(wasmSectionCode, code...),
	)
}

// FR-C-10: compilation runs under a deadline and an expired deadline stops
// it without leaking the runtime or its goroutines.
func TestCompileDeadline(t *testing.T) {
	mod := manyFuncsModule(20000)
	checked, err := checkWasmModule(mod)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	newRT := func() wazero.Runtime {
		return wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(maxWasmMemoryPages).WithCloseOnContextDone(true))
	}

	r := newRT()
	start := time.Now()
	if _, err := compileGuest(ctx, r, checked, CompileTimeout); err != nil {
		t.Fatalf("control compile: %v", err)
	}
	full := time.Since(start)
	r.Close(ctx)

	if !compilerInUse() {
		t.Skip("wazero uses its interpreter here; compilation is not interruptible (bounded by module size)")
	}
	r = newRT()
	start = time.Now()
	_, err = compileGuest(ctx, r, checked, time.Nanosecond)
	stopped := time.Since(start)
	r.Close(ctx)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired deadline: err = %v, want DeadlineExceeded", err)
	}
	t.Logf("full compile %v, with expired deadline %v", full, stopped)

	// NewSandbox with a cancelled context fails and leaves no goroutines.
	runtime.GC()
	before := runtime.NumGoroutine()
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	m := Manifest{ID: "many", Name: "Many", Version: "1.0.0", WasmFile: "plugin.wasm"}
	for i := 0; i < 5; i++ {
		if sb, err := NewSandbox(cctx, m, approvedState(), mod, nil, nil, nil, nil); err == nil {
			sb.Close()
			t.Fatal("NewSandbox compiled under a cancelled context")
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("goroutines grew from %d to %d", before, after)
	}
}

// compilerInUse mirrors wazero's choice of its optimizing compiler
// (amd64/arm64 on the main operating systems); elsewhere it interprets.
func compilerInUse() bool {
	switch runtime.GOOS {
	case "darwin", "linux", "windows", "freebsd", "netbsd", "dragonfly", "solaris", "illumos":
		return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	}
	return false
}

// FuzzCheckWasmModule: the checker never panics, its output passes the
// checker again, and anything it accepts either fails wazero compilation
// or compiles quickly within a bounded allocation. `go test` runs only the
// seeds; use -fuzz=FuzzCheckWasmModule to explore.
func FuzzCheckWasmModule(f *testing.F) {
	for _, w := range [][]byte{
		wasmBenign, wasmLoopStart, wasmLoopHook, wasmFlash, wasmTableGrow, wasmBigTable,
		wasmLocalsBomb, elemDesyncModule(0x80, 0x80, 0x10), manyFuncsModule(3),
		cat(hdr, memSec, memExport, wsec(wasmSectionElement, 0x01, 0x05, 0x70, 0x01, 0xd2, 0x00, 0x0b)),
		cat(hdr, memSec, memExport, wsec(wasmSectionData, 0x01, 0x00, 0x41, 0x00, 0x0b, 0x02, 'h', 'i')),
		cat(hdr, wsec(wasmSectionGlobal, 0x01, 0x7f, 0x01, 0x41, 0x05, 0x0b), memSec, memExport),
	} {
		f.Add(w)
	}
	ctx := context.Background()
	f.Fuzz(func(t *testing.T, data []byte) {
		out, err := checkWasmModule(data)
		if err != nil {
			return
		}
		if _, err := checkWasmModule(out); err != nil {
			t.Fatalf("checker rejects its own output: %v", err)
		}
		r := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().WithMemoryLimitPages(maxWasmMemoryPages))
		defer r.Close(ctx)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		cm, err := compileGuest(ctx, r, out, 5*time.Second)
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		if err == nil {
			cm.Close(ctx)
		}
		if used := after.TotalAlloc - before.TotalAlloc; used > 64<<20+uint64(len(out))*64 {
			t.Fatalf("compiling a %d-byte accepted module allocated %d MiB (err=%v)", len(out), used>>20, err)
		}
		if elapsed > 5*time.Second {
			t.Fatalf("compiling a %d-byte accepted module took %v", len(out), elapsed)
		}
	})
}
