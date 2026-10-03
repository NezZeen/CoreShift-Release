// Command coreshift-release prepares the files of a self-update release.
//
//	coreshift-release keygen -out FILE
//	    creates the release signing key; prints its public key, which goes
//	    into internal/selfupdate/keys.go
//	coreshift-release manifest -installer SETUP.exe -version 1.2.3 -build N [-commit C] -key FILE -out DIR
//	    writes latest.json and latest.json.sig into DIR and copies the
//	    installer there: the three assets of the release; for an APK they
//	    are latest-android.json and latest-android.json.sig
//
// Keep the key out of the repository: whoever has it can make every
// installed CoreShift run their installer as SYSTEM.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"coreshift/engine/internal/selfupdate"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "keygen":
		err = keygen(os.Args[2:])
	case "manifest":
		err = manifest(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: coreshift-release keygen -out FILE | manifest -installer SETUP.exe -version 1.2.3 -build N [-commit C] -key FILE -out DIR")
	os.Exit(2)
}

func keygen(args []string) error {
	fs := flag.NewFlagSet("keygen", flag.ExitOnError)
	out := fs.String("out", "", "file for the private key")
	fs.Parse(args)
	if *out == "" {
		return errors.New("-out is required")
	}
	if _, err := os.Stat(*out); err == nil {
		return fmt.Errorf("%s exists; a new key would stop installed copies from accepting updates signed with it", *out)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
		return err
	}
	fmt.Println(base64.StdEncoding.EncodeToString(pub))
	return nil
}

func manifest(args []string) error {
	fs := flag.NewFlagSet("manifest", flag.ExitOnError)
	installer := fs.String("installer", "", "the installer")
	version := fs.String("version", "", "version, 1.2.3")
	build := fs.Int("build", 0, "build number")
	commit := fs.String("commit", "", "commit")
	notes := fs.String("notes", "", "release notes")
	keyFile := fs.String("key", "", "the private key file")
	out := fs.String("out", "", "folder for the release files")
	fs.Parse(args)
	if *installer == "" || *version == "" || *build <= 0 || *keyFile == "" || *out == "" {
		return errors.New("-installer, -version, -build, -key and -out are required")
	}
	key, err := os.ReadFile(*keyFile)
	if err != nil {
		return err
	}
	pub, err := selfupdate.PublicKey(string(key))
	if err != nil {
		return err
	}
	if !slices.Contains(selfupdate.PublicKeys, pub) {
		return errors.New("this key's public key is not in internal/selfupdate/keys.go: installed copies would reject the release")
	}
	fi, err := os.Stat(*installer)
	if err != nil {
		return err
	}
	sum, err := selfupdate.FileSHA256(*installer)
	if err != nil {
		return err
	}
	m := selfupdate.Manifest{
		Version: *version, Build: *build, Commit: *commit,
		Installer: filepath.Base(*installer), SHA256: sum, Size: fi.Size(),
		Published: time.Now().UTC().Truncate(time.Second), Notes: *notes,
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	sig, err := selfupdate.Sign(body, string(key))
	if err != nil {
		return err
	}
	if _, err := selfupdate.Verify(body, sig, selfupdate.PublicKeys); err != nil {
		return fmt.Errorf("the manifest does not verify: %w", err)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	name := selfupdate.ManifestFor("windows")
	switch filepath.Ext(*installer) {
	case ".apk":
		name = selfupdate.ManifestFor("android")
	case ".deb":
		// Linux: announced, not installed; the .deb is the package the
		// other formats are built with.
		name = selfupdate.ManifestFor("linux")
	}
	if err := os.WriteFile(filepath.Join(*out, name), body, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, name+".sig"), sig, 0o644); err != nil {
		return err
	}
	return copyFile(*installer, filepath.Join(*out, m.Installer))
}

func copyFile(src, dst string) error {
	if abs1, _ := filepath.Abs(src); abs1 == dst {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
