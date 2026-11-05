package serverroute

import (
	"errors"
	"net/netip"
	"os"
	"syscall"
	"testing"
)

// routeNeedsPrivileges reports whether err means the test runner cannot
// manage host routes: no administrator rights on Windows, no CAP_NET_ADMIN
// on Linux.
func routeNeedsPrivileges(err error) bool {
	return errors.Is(err, os.ErrPermission) ||
		errors.Is(err, syscall.EPERM) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.Errno(740)) // ERROR_ELEVATION_REQUIRED
}

// TestAddDel uses TEST-NET-3 (documentation range), which routes nowhere
// real. Unprivileged runners skip.
func TestAddDel(t *testing.T) {
	ip := netip.MustParseAddr("203.0.113.7")
	r, err := Add(ip)
	if err != nil {
		if routeNeedsPrivileges(err) {
			t.Skipf("no permission to manage routes: %v", err)
		}
		t.Fatal(err)
	}
	if r.IP != ip || !r.Gateway.IsValid() || r.IfIndex == 0 || r.IfName == "" {
		t.Fatalf("bad route %+v", r)
	}
	if !routeExists(r) {
		t.Fatalf("Add succeeded, but the system has no route %+v", r)
	}
	// A second Add replaces the first, so switching servers needs no Del.
	if _, err := Add(ip); err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if err := Del(r); err != nil {
		t.Fatalf("Del: %v", err)
	}
	if routeExists(r) {
		t.Fatalf("Del succeeded, but the route %+v is still there", r)
	}
	// Removing a route that is gone is not an error.
	if err := Del(r); err != nil {
		t.Fatalf("second Del: %v", err)
	}
}

func TestAddRejectsBadIPs(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "0.0.0.0", "::1", "::", "2001:db8::1"} {
		if _, err := Add(netip.MustParseAddr(s)); err == nil {
			t.Errorf("Add(%s) succeeded, want error", s)
		}
	}
	if _, err := Add(netip.Addr{}); err == nil {
		t.Error("Add(invalid) succeeded, want error")
	}
}
