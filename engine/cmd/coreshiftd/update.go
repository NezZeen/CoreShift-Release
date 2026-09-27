package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"coreshift/engine/internal/selfupdate"
	"coreshift/engine/internal/service"
)

// runUpdate checks what self-update would find: after publishing a release,
// or when installed copies do not update.
func runUpdate(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "check" {
		return errors.New("usage: coreshiftd update check [-source github:OWNER/REPO|DIR] [-download DIR]")
	}
	fs := flag.NewFlagSet("update check", flag.ExitOnError)
	source := fs.String("source", selfupdate.DefaultSource, "where releases are published")
	download := fs.String("download", "", "also download and verify the installer into this folder")
	platform := fs.String("platform", runtime.GOOS, "whose release to look for: windows or android")
	fs.Parse(args[1:])
	src, err := selfupdate.ParseSource(*source)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	rel, err := selfupdate.Check(ctx, client, src, selfupdate.ManifestFor(*platform), selfupdate.PublicKeys)
	if err != nil {
		return err
	}
	fmt.Printf("latest:    %s, %s, published %s\n", rel.Label(), rel.Installer, rel.Published.Local().Format(time.DateTime))
	fmt.Printf("this one:  %s\n", service.VersionString())
	fmt.Printf("newer:     %v\n", rel.Newer(service.Version, service.BuildNumber()))
	if *download != "" {
		path, err := selfupdate.Download(ctx, client, rel, *download)
		if err != nil {
			return err
		}
		fmt.Printf("verified:  %s\n", path)
	}
	return nil
}
