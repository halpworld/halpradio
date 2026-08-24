//go:build windows

package desktop

import "os"

// isOwnedByCurrentUser on Windows returns true as Unix UID ownership semantics don't apply.
func isOwnedByCurrentUser(fi os.FileInfo) bool {
	return true
}
