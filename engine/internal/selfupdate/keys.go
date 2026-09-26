package selfupdate

// PublicKeys verify releases: base64 Ed25519 public keys. The private key is
// kept by whoever makes releases (see packaging/README.md). To change keys,
// ship a release, signed with the old key, that lists both.
var PublicKeys = []string{
	"iVJUCSZkylixcX7NndSfrAIqzbO1TV8z4y2+Ux/NYBs=", // 2026-09-27
}
