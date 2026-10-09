//go:build !windows

package socksgate

import "syscall"

// exclusive needs nothing here: Linux and Android let another socket into
// a listening port only when both set SO_REUSEPORT (and, from another
// user, not even then), and the gate does not.
func exclusive(_, _ string, _ syscall.RawConn) error { return nil }
