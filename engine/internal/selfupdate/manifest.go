// Package selfupdate finds, downloads and verifies new versions of
// CoreShift itself.
//
// A release publishes three files side by side for each platform: the
// installer, latest.json describing it and latest.json.sig, an Ed25519
// signature of latest.json's exact bytes; for Android they are the APK,
// latest-android.json and latest-android.json.sig. They are the assets of a
// release of a private GitHub repository, read through the API with a
// read-only token built into the service, or files in a folder (for
// testing). A release may carry one platform only: each platform takes the
// newest release that has its files.
//
// The installer runs as SYSTEM, so nothing is trusted that the release key
// did not sign: the manifest's signature is checked against the public keys
// built in here, and the installer against the manifest's SHA-256. The
// token only grants reading the releases; whoever extracts it from the
// binary gets the installers, nothing more.
//
// Neither the private key nor the token enters the repository; see
// packaging/README.md.
package selfupdate

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultSource is where releases are published: "github:OWNER/REPO", or
// a folder holding the three files.
const DefaultSource = "github:NezZeen/coreshift-releases"

// PublicSource is where Linux looks for new versions: the public
// repository of downloads (packaging/linux/README.md). Linux builds carry no
// releases token, and they only tell the user about a new version, whose
// page is then opened in the browser; nothing is installed. The manifest is
// verified with the same keys all the same, so a release in it must have
// been signed by us.
const PublicSource = "github-public:NezZeen/CoreShift-Release"

// DefaultSourceFor is where releases for goos are looked for when the
// settings name no source.
func DefaultSourceFor(goos string) string {
	if goos == "linux" {
		return PublicSource
	}
	return DefaultSource
}

// ManifestName and SignatureName are the published file names of the
// Windows release; AndroidManifestName is the Android one's manifest.
// LinuxManifestName describes the Linux release by its .deb: Linux only
// announces new versions (its package manager installs them), and the
// name of its own keeps a Linux daemon from ever taking the Windows
// manifest.
const (
	ManifestName        = "latest.json"
	SignatureName       = ManifestName + ".sig"
	AndroidManifestName = "latest-android.json"
	LinuxManifestName   = "latest-linux.json"
)

// ManifestFor is the manifest of the release for goos.
func ManifestFor(goos string) string {
	switch goos {
	case "android":
		return AndroidManifestName
	case "linux":
		return LinuxManifestName
	}
	return ManifestName
}

// installerExt is the kind of installer a manifest may name: a manifest
// signed for one platform cannot hand another platform's file to it.
func installerExt(manifest string) string {
	switch manifest {
	case AndroidManifestName:
		return ".apk"
	case LinuxManifestName:
		return ".deb"
	}
	return ".exe"
}

// maxInstallerSize bounds a download; the installer is about 65 MB, the
// APK 80 MB.
const maxInstallerSize = 512 << 20

// Manifest describes a release.
type Manifest struct {
	// Version is the number from the VERSION file, "0.2.2".
	Version string `json:"version"`
	// Build is the commit count, as stamped into the binaries.
	Build  int    `json:"build"`
	Commit string `json:"commit,omitempty"`
	// Installer is the installer's file name, next to the manifest.
	Installer string `json:"installer"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	// Published is when the release was made.
	Published time.Time `json:"published"`
	Notes     string    `json:"notes,omitempty"`
}

// Label is how the app shows a version: "0.2.2 (build 5)".
func (m Manifest) Label() string { return fmt.Sprintf("%s (build %d)", m.Version, m.Build) }

var (
	versionRE   = regexp.MustCompile(`^\d+\.\d+\.\d+$`)
	sha256RE    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	installerRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.(exe|apk|deb)$`)
)

// Verify checks sig, the signature of manifest, against keys and parses
// the manifest. keys are base64 Ed25519 public keys.
func Verify(manifest, sig []byte, keys []string) (Manifest, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(raw) != ed25519.SignatureSize {
		return Manifest{}, errors.New("update signature is malformed")
	}
	ok := false
	for _, k := range keys {
		pub, err := base64.StdEncoding.DecodeString(k)
		if err == nil && len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, manifest, raw) {
			ok = true
			break
		}
	}
	if !ok {
		return Manifest{}, errors.New("update signature does not match the release key")
	}
	var m Manifest
	if err := json.Unmarshal(manifest, &m); err != nil {
		return Manifest{}, fmt.Errorf("update manifest: %w", err)
	}
	switch {
	case !versionRE.MatchString(m.Version):
		return Manifest{}, fmt.Errorf("update manifest: bad version %q", m.Version)
	case m.Build <= 0:
		return Manifest{}, fmt.Errorf("update manifest: bad build %d", m.Build)
	case !installerRE.MatchString(m.Installer):
		// A plain file name: it can neither leave the release nor the
		// download folder.
		return Manifest{}, fmt.Errorf("update manifest: bad installer name %q", m.Installer)
	case !sha256RE.MatchString(m.SHA256):
		return Manifest{}, errors.New("update manifest: bad SHA-256")
	case m.Size <= 0 || m.Size > maxInstallerSize:
		return Manifest{}, fmt.Errorf("update manifest: bad size %d", m.Size)
	}
	return m, nil
}

// Sign signs manifest with a base64 Ed25519 private key (seed or full key),
// for the release tool.
func Sign(manifest []byte, privateKey string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privateKey))
	if err != nil {
		return nil, errors.New("private key is not base64")
	}
	var priv ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		priv = raw
	default:
		return nil, errors.New("private key has the wrong size")
	}
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, manifest)) + "\n"), nil
}

// PublicKey returns the base64 public key of a base64 private key.
func PublicKey(privateKey string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privateKey))
	if err != nil || (len(raw) != ed25519.SeedSize && len(raw) != ed25519.PrivateKeySize) {
		return "", errors.New("not a base64 Ed25519 private key")
	}
	pub := ed25519.NewKeyFromSeed(raw[:ed25519.SeedSize]).Public().(ed25519.PublicKey)
	return base64.StdEncoding.EncodeToString(pub), nil
}

// Newer reports whether m is later than version and build: by the version
// number, then by build.
func (m Manifest) Newer(version string, build int) bool {
	a, b := numbers(m.Version), numbers(version)
	for i := range 3 {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return m.Build > build
}

func numbers(v string) [3]int {
	var n [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		n[i], _ = strconv.Atoi(p)
	}
	return n
}
