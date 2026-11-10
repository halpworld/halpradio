//go:build windows

package debuglog

import "os"

// Windows has no O_NOFOLLOW; Init rejects a symlink with Lstat before opening.
func openLogFile(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
}

// File ownership is governed by ACLs on Windows; the per-user profile
// directory is the protection there.
func ownedByCurrentUser(os.FileInfo) bool { return true }
