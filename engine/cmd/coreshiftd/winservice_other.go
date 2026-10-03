//go:build !windows && (!linux || android)

package main

import (
	"context"
	"errors"
	"os"
)

func isElevated() bool { return os.Geteuid() == 0 }

func runService(context.Context, []string) error {
	return errors.New("no system service on this platform; run `coreshiftd serve` instead")
}
