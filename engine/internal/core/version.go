package core

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var versionRe = regexp.MustCompile(`\bv?(\d+\.\d+\.\d+(?:[-.][0-9A-Za-z.]+)?)\b`)

// ParseVersion finds the version in what a core prints for VersionArgs:
// "Xray 26.3.27 (…)", "sing-box version 1.14.2", "Mihomo Meta v1.19.31 …".
func ParseVersion(out string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	if m := versionRe.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// Version runs the core at bin and returns its version.
func Version(ctx context.Context, k Kind, bin string) (string, error) {
	a, ok := ByKind(k)
	if !ok {
		return "", fmt.Errorf("unknown core %q", k)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, a.VersionArgs()...)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", k, err)
	}
	v := ParseVersion(string(out))
	if v == "" {
		return "", fmt.Errorf("%s: no version in %q", k, strings.TrimSpace(string(out)))
	}
	return v, nil
}

// Check has the core at bin validate the config at path without running
// it: an updated core may no longer take a config the old one did.
func Check(ctx context.Context, k Kind, bin, path, workDir string) error {
	a, ok := ByKind(k)
	if !ok {
		return fmt.Errorf("unknown core %q", k)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, a.CheckArgs(path, workDir)...)
	cmd.Dir = workDir
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	// The last line is where a core says why it refused the config.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return fmt.Errorf("%s: %w: %s", k, err, last)
	}
	return fmt.Errorf("%s: %w", k, err)
}
