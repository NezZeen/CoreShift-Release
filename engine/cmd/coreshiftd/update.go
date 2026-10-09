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
		return errors.New("usage: coreshiftd update check [-platform windows|android|linux] [-source github:OWNER/REPO|github-public:OWNER/REPO|gitlab-public:NAMESPACE/PROJECT|DIR] [-download DIR]")
	}
	fs := flag.NewFlagSet("update check", flag.ExitOnError)
	source := fs.String("source", "", "where releases are published (default: GitHub, then the GitLab mirror if GitHub is out of reach)")
	download := fs.String("download", "", "also download and verify the installer into this folder")
	platform := fs.String("platform", runtime.GOOS, "whose release to look for: windows, android or linux")
	fs.Parse(args[1:])
	// As the service does: the mirror only when no source is named and
	// GitHub cannot be reached.
	sources := []string{*source}
	if *source == "" {
		sources = []string{selfupdate.DefaultSourceFor(*platform), selfupdate.MirrorSourceFor(*platform)}
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	var rel selfupdate.Release
	for i, name := range sources {
		src, err := selfupdate.ParseSource(name)
		if err != nil {
			return err
		}
		rel, err = selfupdate.Check(ctx, client, src, selfupdate.ManifestFor(*platform), selfupdate.PublicKeys)
		if err == nil {
			break
		}
		if i == len(sources)-1 || !selfupdate.Unreachable(err) {
			return err
		}
		fmt.Printf("%s: %v; trying %s\n", src, err, sources[i+1])
	}
	fmt.Printf("source:    %s\n", rel.Source())
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
