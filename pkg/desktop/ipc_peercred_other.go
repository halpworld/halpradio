//go:build !linux && !darwin && !freebsd

package desktop

import "net"

// checkPeerCredentials has no portable peer-credential API on this platform;
// access rests on the socket's 0600 mode / directory ACL.
func checkPeerCredentials(net.Conn) error { return nil }
