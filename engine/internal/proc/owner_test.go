package proc

import (
	"errors"
	"net"
	"net/netip"
	"os"
	"testing"
)

// The listener on a port is told to be this process's and no other's; a
// port nobody listens on is nobody's.
func TestListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := netip.MustParseAddrPort(ln.Addr().String())
	owned, err := Listens(os.Getpid(), addr)
	if errors.Is(err, ErrOwnerUnknown) {
		t.Skip(err)
	}
	if err != nil || !owned {
		t.Fatalf("own listener: %v, %v", owned, err)
	}
	if owned, err := Listens(os.Getppid(), addr); err != nil || owned {
		t.Errorf("the parent's: %v, %v", owned, err)
	}
	ln.Close()
	if owned, err := Listens(os.Getpid(), addr); err != nil || owned {
		t.Errorf("after closing: %v, %v", owned, err)
	}
}
