//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
)

func isElevated() bool { return os.Geteuid() == 0 }

func runService(context.Context, []string) error {
	return errors.New("on Linux run the daemon with systemd: see packaging/linux/coreshiftd.service")
}
