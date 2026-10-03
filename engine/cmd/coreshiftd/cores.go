package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"coreshift/engine/internal/core"
	"coreshift/engine/internal/coreupdate"
)

// coreFile is the executable's name in a cores folder of this platform,
// as findCores knows it.
func coreFile(k core.Kind, goos string) string {
	name := string(k)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// runCores fetches the cores for a build (packaging/linux/build.sh): the
// latest stable release of each from GitHub, for this OS and processor,
// checked against GitHub's SHA-256 and started once to read its version,
// as the daemon's own core updates are.
//
//	coreshiftd cores fetch -dir DIR [-only xray,sing-box,mihomo]
func runCores(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "fetch" {
		return errors.New("usage: coreshiftd cores fetch -dir DIR [-only xray,sing-box,mihomo]")
	}
	fs := flag.NewFlagSet("cores fetch", flag.ExitOnError)
	dir := fs.String("dir", "", "folder for the cores, e.g. engine/testdata/bin/linux-amd64")
	only := fs.String("only", "xray,sing-box,mihomo", "cores to fetch")
	fs.Parse(args[1:])
	if *dir == "" {
		return errors.New("-dir is required")
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	for _, k := range parseKinds(*only) {
		rel, err := coreupdate.Latest(ctx, client, k)
		if err != nil {
			return err
		}
		bin := filepath.Join(*dir, coreFile(k, runtime.GOOS))
		fmt.Printf("%s %s (%s)... ", k, rel.Version, rel.Asset)
		v, err := coreupdate.Install(ctx, client, rel, bin)
		if err != nil {
			fmt.Println()
			return err
		}
		fmt.Printf("ok, version %s\n", v)
	}
	// The versions replaced, if the folder had cores already.
	coreupdate.Cleanup(map[core.Kind]string{core.Xray: filepath.Join(*dir, coreFile(core.Xray, runtime.GOOS))})
	return nil
}
