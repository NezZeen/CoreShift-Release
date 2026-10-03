//go:build !windows

package selfupdate

// checkLocal has nothing to check here: reading a path does not sign in
// anywhere.
func checkLocal(string) error { return nil }
