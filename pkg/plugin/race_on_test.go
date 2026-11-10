//go:build race

package plugin

// The race detector slows wazero's compiler 5-20x; a Go toolchain module
// that compiles in under a second can pass CompileTimeout on a shared CI
// runner. Release builds keep CompileTimeout.
func init() { compileTimeout = 10 * CompileTimeout }
