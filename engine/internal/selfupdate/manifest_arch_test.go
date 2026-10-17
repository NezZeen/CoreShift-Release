package selfupdate

import "testing"

// 32-bit ARM phones update from a manifest of their own; every manifest of
// Android names an APK.
func TestManifestForArch(t *testing.T) {
	for _, c := range []struct{ goos, goarch, want string }{
		{"android", "arm64", AndroidManifestName},
		{"android", "arm", AndroidArmManifestName},
		{"android", "", AndroidManifestName},
		{"windows", "amd64", ManifestName},
		{"linux", "arm", LinuxManifestName},
	} {
		if got := ManifestForArch(c.goos, c.goarch); got != c.want {
			t.Errorf("%s/%s: %s, want %s", c.goos, c.goarch, got, c.want)
		}
	}
	if installerExt(AndroidArmManifestName) != ".apk" {
		t.Error("the 32-bit manifest may name something else than an APK")
	}
}
