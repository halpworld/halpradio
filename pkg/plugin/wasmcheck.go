package plugin

import (
	"errors"
	"fmt"
)

// Resource limits enforced on a plugin binary before wazero sees it.
//
// WithMemoryLimitPages caps linear memory only. wazero allocates other
// structures straight from counts the module declares: tables
// (make([]uintptr, min) at instantiation, table.grow up to 2^32 entries
// when no maximum is declared), locals (make([]ValueType, 0, sum) per
// function), and every section vector (make([]T, count) before a single
// entry is read). A 34-byte module declaring a 2^27-entry table made the
// host allocate 1 GiB. checkWasmModule walks every section, refuses counts
// that are not backed by bytes or exceed these limits, and returns a copy
// of the module that wazero can decode without large allocations.
const (
	maxWasmTypes        = 1 << 14
	maxWasmParams       = 1000
	maxWasmImports      = 1 << 12
	maxWasmFunctions    = 1 << 18
	maxWasmTables       = 8
	maxTableElements    = 1 << 17 // per table; 8 bytes each in wazero
	maxWasmGlobals      = 1 << 14
	maxWasmExports      = 1 << 14
	maxElemSegments     = 1 << 14
	maxElemEntries      = 1 << 18 // across all element segments
	maxDataSegments     = 1 << 16
	maxLocalsPerFunc    = 1 << 15
	maxLocalsTotal      = 1 << 21
	maxWasmNameLen      = 1 << 12
	maxWasmMemoryPages  = 512 // must match WithMemoryLimitPages in NewSandbox
	maxWasmMemories     = 1
	wasmSectionCustom   = 0
	wasmSectionType     = 1
	wasmSectionImport   = 2
	wasmSectionFunction = 3
	wasmSectionTable    = 4
	wasmSectionMemory   = 5
	wasmSectionGlobal   = 6
	wasmSectionExport   = 7
	wasmSectionStart    = 8
	wasmSectionElement  = 9
	wasmSectionCode     = 10
	wasmSectionData     = 11
	wasmSectionDataCnt  = 12
)

var errWasmTruncated = errors.New("unexpected end of section")

// wasmReader is a bounds-checked cursor over one section.
type wasmReader struct {
	b   []byte
	pos int
}

func (r *wasmReader) remaining() int { return len(r.b) - r.pos }

func (r *wasmReader) byte() (byte, error) {
	if r.pos >= len(r.b) {
		return 0, errWasmTruncated
	}
	c := r.b[r.pos]
	r.pos++
	return c, nil
}

func (r *wasmReader) skip(n int) error {
	if n < 0 || n > r.remaining() {
		return errWasmTruncated
	}
	r.pos += n
	return nil
}

// leb reads an unsigned or signed LEB128 value of at most maxBytes bytes
// and returns its unsigned interpretation (only used for unsigned fields).
func (r *wasmReader) leb(maxBytes int) (uint64, error) {
	var v uint64
	for i := 0; i < maxBytes; i++ {
		c, err := r.byte()
		if err != nil {
			return 0, err
		}
		v |= uint64(c&0x7f) << (7 * uint(i))
		if c&0x80 == 0 {
			return v, nil
		}
	}
	return 0, errors.New("LEB128 value too long")
}

func (r *wasmReader) u32() (uint32, error) {
	v, err := r.leb(5)
	if err != nil {
		return 0, err
	}
	if v > 0xffffffff {
		return 0, errors.New("u32 out of range")
	}
	return uint32(v), nil
}

// count reads a vector length and checks it against limit and against the
// bytes left, given that each entry needs at least minEntry bytes.
func (r *wasmReader) count(what string, limit, minEntry int) (int, error) {
	n, err := r.u32()
	if err != nil {
		return 0, fmt.Errorf("%s count: %w", what, err)
	}
	if uint64(n) > uint64(limit) {
		return 0, fmt.Errorf("%s count %d exceeds limit %d", what, n, limit)
	}
	if minEntry > 0 && uint64(n)*uint64(minEntry) > uint64(r.remaining()) {
		return 0, fmt.Errorf("%s count %d is larger than the section", what, n)
	}
	return int(n), nil
}

func (r *wasmReader) name() error {
	n, err := r.count("name", maxWasmNameLen, 1)
	if err != nil {
		return err
	}
	return r.skip(n)
}

func isWasmValType(c byte) bool {
	switch c {
	case 0x7f, 0x7e, 0x7d, 0x7c, 0x7b, 0x70, 0x6f:
		return true
	}
	return false
}

func isWasmRefType(c byte) bool { return c == 0x70 || c == 0x6f }

// limits reads a limits pair (flag 0: min, flag 1: min+max). Shared or
// 64-bit memories need features that are not enabled and are refused.
func (r *wasmReader) limits() (min uint32, max uint32, hasMax bool, err error) {
	flag, err := r.byte()
	if err != nil {
		return 0, 0, false, err
	}
	if flag > 1 {
		return 0, 0, false, fmt.Errorf("unsupported limits flag 0x%02x", flag)
	}
	if min, err = r.u32(); err != nil {
		return 0, 0, false, err
	}
	if flag == 1 {
		if max, err = r.u32(); err != nil {
			return 0, 0, false, err
		}
		if max < min {
			return 0, 0, false, fmt.Errorf("limits max %d below min %d", max, min)
		}
		hasMax = true
	}
	return min, max, hasMax, nil
}

// constExpr skips a constant expression (WebAssembly 2.0 forms only).
func (r *wasmReader) constExpr() error {
	for {
		op, err := r.byte()
		if err != nil {
			return err
		}
		switch op {
		case 0x0b: // end
			return nil
		case 0x41: // i32.const
			_, err = r.leb(5)
		case 0x42: // i64.const
			_, err = r.leb(10)
		case 0x43: // f32.const
			err = r.skip(4)
		case 0x44: // f64.const
			err = r.skip(8)
		case 0x23, 0xd2: // global.get, ref.func
			_, err = r.u32()
		case 0xd0: // ref.null
			var t byte
			if t, err = r.byte(); err == nil && !isWasmRefType(t) {
				err = fmt.Errorf("invalid ref.null type 0x%02x", t)
			}
		case 0xfd: // v128.const
			var sub uint32
			if sub, err = r.u32(); err == nil {
				if sub != 12 {
					err = fmt.Errorf("unsupported SIMD op %d in constant expression", sub)
				} else {
					err = r.skip(16)
				}
			}
		default:
			return fmt.Errorf("unsupported opcode 0x%02x in constant expression", op)
		}
		if err != nil {
			return err
		}
	}
}

// checkWasmModule validates the binary structure and resource declarations
// of a plugin module. It returns the module to compile: custom sections
// (debug names and producer info, which wazero decodes with unbounded
// counts) are dropped, and table maximums are clamped to maxTableElements
// so table.grow fails cleanly instead of allocating host memory.
func checkWasmModule(wasm []byte) ([]byte, error) {
	if len(wasm) < 8 || string(wasm[:4]) != "\x00asm" {
		return nil, errors.New("not a WebAssembly binary")
	}
	if wasm[4] != 1 || wasm[5] != 0 || wasm[6] != 0 || wasm[7] != 0 {
		return nil, errors.New("unsupported WebAssembly binary version")
	}

	out := make([]byte, 0, len(wasm))
	out = append(out, wasm[:8]...)

	top := &wasmReader{b: wasm, pos: 8}
	seen := map[byte]bool{}
	var importedFuncs, funcDecls int

	for top.remaining() > 0 {
		id, err := top.byte()
		if err != nil {
			return nil, err
		}
		size, err := top.u32()
		if err != nil {
			return nil, fmt.Errorf("section %d size: %w", id, err)
		}
		if int64(size) > int64(top.remaining()) {
			return nil, fmt.Errorf("section %d is larger than the file", id)
		}
		body := wasm[top.pos : top.pos+int(size)]
		top.pos += int(size)

		if id == wasmSectionCustom {
			continue // dropped: not needed to run the module
		}
		if id > wasmSectionDataCnt {
			return nil, fmt.Errorf("unsupported section id %d", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate section id %d", id)
		}
		seen[id] = true

		r := &wasmReader{b: body}
		var rewritten []byte
		switch id {
		case wasmSectionType:
			err = checkTypeSection(r)
		case wasmSectionImport:
			importedFuncs, err = checkImportSection(r)
		case wasmSectionFunction:
			funcDecls, err = checkFunctionSection(r, importedFuncs)
		case wasmSectionTable:
			rewritten, err = checkTableSection(r)
		case wasmSectionMemory:
			err = checkMemorySection(r)
		case wasmSectionGlobal:
			err = checkGlobalSection(r)
		case wasmSectionExport:
			err = checkExportSection(r)
		case wasmSectionStart:
			_, err = r.u32()
		case wasmSectionElement:
			err = checkElementSection(r)
		case wasmSectionCode:
			err = checkCodeSection(r, funcDecls)
		case wasmSectionData:
			err = checkDataSection(r)
		case wasmSectionDataCnt:
			var n uint32
			if n, err = r.u32(); err == nil && n > maxDataSegments {
				err = fmt.Errorf("data count %d exceeds limit %d", n, maxDataSegments)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("section %d: %w", id, err)
		}
		if r.remaining() != 0 {
			return nil, fmt.Errorf("section %d: %d trailing bytes", id, r.remaining())
		}
		if rewritten != nil {
			body = rewritten
		}
		out = append(out, id)
		out = appendULEB(out, uint64(len(body)))
		out = append(out, body...)
	}
	return out, nil
}

func checkTypeSection(r *wasmReader) error {
	n, err := r.count("type", maxWasmTypes, 3)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		form, err := r.byte()
		if err != nil {
			return err
		}
		if form != 0x60 {
			return fmt.Errorf("type %d: unsupported form 0x%02x", i, form)
		}
		for _, what := range []string{"param", "result"} {
			k, err := r.count(what, maxWasmParams, 1)
			if err != nil {
				return err
			}
			for j := 0; j < k; j++ {
				c, err := r.byte()
				if err != nil {
					return err
				}
				if !isWasmValType(c) {
					return fmt.Errorf("type %d: invalid value type 0x%02x", i, c)
				}
			}
		}
	}
	return nil
}

// checkImportSection allows function imports only. The host exposes no
// tables, memories or globals, so any other import could never link; it is
// refused here before wazero sizes it.
func checkImportSection(r *wasmReader) (int, error) {
	n, err := r.count("import", maxWasmImports, 4)
	if err != nil {
		return 0, err
	}
	for i := 0; i < n; i++ {
		if err := r.name(); err != nil {
			return 0, err
		}
		if err := r.name(); err != nil {
			return 0, err
		}
		kind, err := r.byte()
		if err != nil {
			return 0, err
		}
		if kind != 0x00 {
			return 0, fmt.Errorf("import %d: only function imports are allowed (kind %d)", i, kind)
		}
		if _, err := r.u32(); err != nil {
			return 0, err
		}
	}
	return n, nil
}

func checkFunctionSection(r *wasmReader, imported int) (int, error) {
	n, err := r.count("function", maxWasmFunctions-imported, 1)
	if err != nil {
		return 0, err
	}
	for i := 0; i < n; i++ {
		if _, err := r.u32(); err != nil {
			return 0, err
		}
	}
	return n, nil
}

// checkTableSection bounds table sizes and rebuilds the section with every
// maximum clamped to maxTableElements.
func checkTableSection(r *wasmReader) ([]byte, error) {
	n, err := r.count("table", maxWasmTables, 3)
	if err != nil {
		return nil, err
	}
	out := appendULEB(nil, uint64(n))
	for i := 0; i < n; i++ {
		ref, err := r.byte()
		if err != nil {
			return nil, err
		}
		if !isWasmRefType(ref) {
			return nil, fmt.Errorf("table %d: invalid element type 0x%02x", i, ref)
		}
		min, max, hasMax, err := r.limits()
		if err != nil {
			return nil, fmt.Errorf("table %d: %w", i, err)
		}
		if min > maxTableElements {
			return nil, fmt.Errorf("table %d: minimum %d entries exceeds limit %d", i, min, maxTableElements)
		}
		if !hasMax || max > maxTableElements {
			max = maxTableElements
		}
		out = append(out, ref, 0x01)
		out = appendULEB(out, uint64(min))
		out = appendULEB(out, uint64(max))
	}
	return out, nil
}

func checkMemorySection(r *wasmReader) error {
	n, err := r.count("memory", maxWasmMemories, 2)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		min, max, hasMax, err := r.limits()
		if err != nil {
			return fmt.Errorf("memory %d: %w", i, err)
		}
		if min > maxWasmMemoryPages {
			return fmt.Errorf("memory %d: minimum %d pages exceeds limit %d", i, min, maxWasmMemoryPages)
		}
		if hasMax && max > maxWasmMemoryPages {
			return fmt.Errorf("memory %d: maximum %d pages exceeds limit %d", i, max, maxWasmMemoryPages)
		}
	}
	return nil
}

func checkGlobalSection(r *wasmReader) error {
	n, err := r.count("global", maxWasmGlobals, 3)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		t, err := r.byte()
		if err != nil {
			return err
		}
		if !isWasmValType(t) {
			return fmt.Errorf("global %d: invalid type 0x%02x", i, t)
		}
		mut, err := r.byte()
		if err != nil {
			return err
		}
		if mut > 1 {
			return fmt.Errorf("global %d: invalid mutability %d", i, mut)
		}
		if err := r.constExpr(); err != nil {
			return fmt.Errorf("global %d: %w", i, err)
		}
	}
	return nil
}

func checkExportSection(r *wasmReader) error {
	n, err := r.count("export", maxWasmExports, 3)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		if err := r.name(); err != nil {
			return err
		}
		kind, err := r.byte()
		if err != nil {
			return err
		}
		if kind > 3 {
			return fmt.Errorf("export %d: invalid kind %d", i, kind)
		}
		if _, err := r.u32(); err != nil {
			return err
		}
	}
	return nil
}

func checkElementSection(r *wasmReader) error {
	n, err := r.count("element segment", maxElemSegments, 2)
	if err != nil {
		return err
	}
	total := 0
	for i := 0; i < n; i++ {
		flags, err := r.u32()
		if err != nil {
			return err
		}
		if flags > 7 {
			return fmt.Errorf("element segment %d: invalid flags %d", i, flags)
		}
		active := flags&0x1 == 0
		explicitTable := flags&0x2 != 0
		exprs := flags&0x4 != 0
		if active && explicitTable {
			if _, err := r.u32(); err != nil {
				return err
			}
		}
		if active {
			if err := r.constExpr(); err != nil {
				return fmt.Errorf("element segment %d offset: %w", i, err)
			}
		}
		if !active || explicitTable {
			// Index segments carry elemkind 0x00 (funcref); expression
			// segments carry a one-byte reftype. Anything else is refused:
			// wazero also accepts the typed-reference prefixes 0x63/0x64
			// followed by a heap-type LEB, and skipping one byte here would
			// leave this checker and wazero reading different fields
			// (FR-C-4: a 147-byte module made wazero allocate 768 MiB).
			kind, err := r.byte()
			if err != nil {
				return err
			}
			if exprs {
				if !isWasmRefType(kind) {
					return fmt.Errorf("element segment %d: unsupported reference type 0x%02x", i, kind)
				}
			} else if kind != 0x00 {
				return fmt.Errorf("element segment %d: unsupported element kind 0x%02x", i, kind)
			}
		}
		k, err := r.count("element", maxElemEntries-total, 1)
		if err != nil {
			return fmt.Errorf("element segment %d: %w", i, err)
		}
		total += k
		for j := 0; j < k; j++ {
			if exprs {
				err = r.constExpr()
			} else {
				_, err = r.u32()
			}
			if err != nil {
				return fmt.Errorf("element segment %d: %w", i, err)
			}
		}
	}
	return nil
}

func checkCodeSection(r *wasmReader, declared int) error {
	n, err := r.count("code", maxWasmFunctions, 2)
	if err != nil {
		return err
	}
	if n != declared {
		return fmt.Errorf("code count %d does not match function count %d", n, declared)
	}
	total := 0
	for i := 0; i < n; i++ {
		size, err := r.u32()
		if err != nil {
			return err
		}
		if int64(size) > int64(r.remaining()) {
			return fmt.Errorf("function %d body is larger than the section", i)
		}
		body := &wasmReader{b: r.b[r.pos : r.pos+int(size)]}
		r.pos += int(size)

		groups, err := body.count("local group", maxLocalsPerFunc, 2)
		if err != nil {
			return fmt.Errorf("function %d: %w", i, err)
		}
		locals := 0
		for g := 0; g < groups; g++ {
			c, err := body.u32()
			if err != nil {
				return fmt.Errorf("function %d: %w", i, err)
			}
			if uint64(locals)+uint64(c) > maxLocalsPerFunc {
				return fmt.Errorf("function %d: more than %d locals", i, maxLocalsPerFunc)
			}
			locals += int(c)
			t, err := body.byte()
			if err != nil {
				return fmt.Errorf("function %d: %w", i, err)
			}
			if !isWasmValType(t) {
				return fmt.Errorf("function %d: invalid local type 0x%02x", i, t)
			}
		}
		total += locals
		if total > maxLocalsTotal {
			return fmt.Errorf("module declares more than %d locals", maxLocalsTotal)
		}
	}
	return nil
}

func checkDataSection(r *wasmReader) error {
	n, err := r.count("data segment", maxDataSegments, 2)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		flags, err := r.u32()
		if err != nil {
			return err
		}
		switch flags {
		case 0:
			err = r.constExpr()
		case 1:
		case 2:
			if _, err = r.u32(); err == nil {
				err = r.constExpr()
			}
		default:
			return fmt.Errorf("data segment %d: invalid flags %d", i, flags)
		}
		if err != nil {
			return fmt.Errorf("data segment %d: %w", i, err)
		}
		size, err := r.count("data byte", maxWasmDownloadBytes, 1)
		if err != nil {
			return fmt.Errorf("data segment %d: %w", i, err)
		}
		if err := r.skip(size); err != nil {
			return err
		}
	}
	return nil
}

func appendULEB(b []byte, v uint64) []byte {
	for {
		c := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b = append(b, c|0x80)
			continue
		}
		return append(b, c)
	}
}
