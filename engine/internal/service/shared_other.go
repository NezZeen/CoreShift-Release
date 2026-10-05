//go:build !windows

package service

import "context"

// KeepShared keeps the permissions of a file WriteShared wrote up to date
// while ctx lasts. Only Windows needs it (secure_windows.go): there they
// name the members of the API group, who may change. On Linux the group
// alone grants access.
func KeepShared(ctx context.Context, path string) {}
