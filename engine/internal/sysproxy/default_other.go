//go:build !windows && !linux

package sysproxy

import (
	"context"
	"errors"
)

// Default: the system proxy is set on Windows and Linux only.
func Default(context.Context) (*Manager, error) {
	return nil, errors.New("the system proxy is set on Windows and Linux only")
}
