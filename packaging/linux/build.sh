#!/usr/bin/env bash
# Builds CoreShift for Linux on a Linux machine (or CI) of the target
# processor; Flutter cannot cross-build Linux apps:
#
#   dist/coreshift_<version>_<arch>.deb            Debian, Ubuntu, Mint
#   dist/coreshift-<tag>-linux-<arch>.tar.gz       anything with systemd (install.sh)
#
#   packaging/linux/build.sh [--cores DIR] [--out DIR] [--no-deb] [--no-tar] [--fetch-cores]
#
# Needs go, flutter (with the Linux toolchain: clang cmake ninja-build
# pkg-config libgtk-3-dev), git, and dpkg-deb for the .deb. See
# packaging/linux/README.md.
#
# The version is the VERSION file; the build number is the number of
# commits and the commit the short hash, "-dirty" with uncommitted changes,
# as in packaging/windows/build.ps1. Releases (the commit tagged
# v<version>) are named by the version alone.
#
# Cores come from engine/testdata/bin/linux-<arch> (xray, sing-box,
# mihomo); --fetch-cores downloads the latest ones there first.
#
# No releases token is built in: Linux does not update itself (its package
# manager does), so these binaries hold no secret.
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
here="$root/packaging/linux"

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 flutter_arch=x64 ;;
aarch64 | arm64) arch=arm64 flutter_arch=arm64 ;;
*)
	echo "unsupported processor $(uname -m)" >&2
	exit 1
	;;
esac

cores="$root/engine/testdata/bin/linux-$arch"
out="$root/dist"
make_deb=1
make_tar=1
fetch=0
while [ $# -gt 0 ]; do
	case "$1" in
	--cores) cores=$(cd "$2" && pwd) && shift ;;
	--out) out="$2" && shift ;;
	--no-deb) make_deb=0 ;;
	--no-tar) make_tar=0 ;;
	--fetch-cores) fetch=1 ;;
	*)
		sed -n '2,25p' "$0" >&2
		exit 2
		;;
	esac
	shift
done

step() { printf '\033[36m==> %s\033[0m\n' "$*"; }
need() { command -v "$1" >/dev/null 2>&1 || {
	echo "$1 not found: $2" >&2
	exit 1
}; }

need git 'install git'
need go 'install Go 1.26 or newer'
need flutter 'install Flutter and add its bin to PATH'
[ "$make_deb" = 1 ] && need dpkg-deb 'install dpkg (or pass --no-deb)'

version=$(tr -d '[:space:]' <"$root/VERSION")
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
	echo "VERSION must look like 1.2.3, not '$version'" >&2
	exit 1
}
git -C "$root" rev-parse --verify --quiet HEAD >/dev/null || {
	echo "$root is not a git repository with commits: the build number and the commit come from git" >&2
	exit 1
}
build=$(git -C "$root" rev-list --count HEAD)
commit=$(git -C "$root" rev-parse --short=7 HEAD)
[ -n "$(git -C "$root" status --porcelain)" ] && commit="$commit-dirty"
tag="$version"
debversion="$version"
if [ -z "$(git -C "$root" tag --points-at HEAD --list "v$version")" ]; then
	tag="$tag-b$build"
	# Sorts after the release it follows: 0.6.5 < 0.6.5+b130.
	debversion="$debversion+b$build"
fi
if [[ "$commit" == *-dirty ]]; then
	tag="$tag-dirty"
	debversion="$debversion+dirty"
fi

stage="$out/linux-stage"
app="$stage/opt/coreshift"
rm -rf "$stage"
mkdir -p "$app" "$out"

if [ "$fetch" = 1 ]; then
	step "cores: latest releases into $cores"
	(cd "$root/engine" && go run ./cmd/coreshiftd cores fetch -dir "$cores")
fi
for c in xray sing-box mihomo; do
	[ -x "$cores/$c" ] || {
		echo "no $cores/$c: put the linux-$arch cores there or pass --fetch-cores" >&2
		exit 1
	}
done

step "coreshiftd $version build $build ($commit), linux/$arch"
if [ -e "$root/engine/internal/selfupdate/token_gen.go" ]; then
	echo "engine/internal/selfupdate/token_gen.go exists: remove it, Linux builds carry no releases token" >&2
	exit 1
fi
pkg=coreshift/engine/internal/service
(cd "$root/engine" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
	-ldflags "-s -w -X $pkg.Version=$version -X $pkg.Build=$build -X $pkg.Commit=$commit" \
	-o "$app/coreshiftd" ./cmd/coreshiftd)

step 'app'
(cd "$root/app" && flutter build linux --release --build-name "$version" --build-number "$build" \
	"--dart-define=CORESHIFT_VERSION=$version" "--dart-define=CORESHIFT_BUILD=$build" "--dart-define=CORESHIFT_COMMIT=$commit")
cp -a "$root/app/build/linux/$flutter_arch/release/bundle/." "$app/"

step "cores from $cores"
mkdir -p "$app/cores"
# Executables and their licences; nothing of other platforms.
find "$cores" -maxdepth 1 -type f ! -name '*.exe' ! -name '*.ps1' ! -name '*.vbs' ! -name 'README*' ! -name '.*' -exec cp -a {} "$app/cores/" \;
chmod 755 "$app/cores/xray" "$app/cores/sing-box" "$app/cores/mihomo"
for c in xray sing-box mihomo; do
	case "$c" in
	mihomo) "$app/cores/$c" -v >/dev/null ;;
	*) "$app/cores/$c" version >/dev/null ;;
	esac || {
		echo "$c does not start: wrong processor or a broken file" >&2
		exit 1
	}
done

step 'system files'
install -m 755 "$here/coreshift-setup.sh" "$app/coreshift-setup.sh"
install -D -m 644 "$here/coreshift.service" "$stage/usr/lib/systemd/system/coreshift.service"
install -D -m 644 "$here/dev.coreshift.coreshift.desktop" "$stage/usr/share/applications/dev.coreshift.coreshift.desktop"
install -D -m 644 "$root/app/web/icons/Icon-512.png" "$stage/usr/share/icons/hicolor/512x512/apps/coreshift.png"
mkdir -p "$stage/usr/bin"
ln -s /opt/coreshift/coreshift "$stage/usr/bin/coreshift"
ln -s /opt/coreshift/coreshiftd "$stage/usr/bin/coreshiftd"
chmod -R go-w "$stage"

if [ "$make_deb" = 1 ]; then
	step "deb $debversion"
	mkdir -p "$stage/DEBIAN"
	size=$(du -sk --exclude=DEBIAN "$stage" | cut -f1)
	cat >"$stage/DEBIAN/control" <<EOF
Package: coreshift
Version: $debversion
Section: net
Priority: optional
Architecture: $arch
Maintainer: CoreShift <coreshift@localhost>
Installed-Size: $size
Depends: libgtk-3-0 | libgtk-3-0t64, libx11-6, libxi6, systemd, passwd
Recommends: pkexec | policykit-1, libnotify-bin
Suggests: gnome-shell-extension-appindicator
Homepage: https://github.com/NezZeen/CoreShift-Release
Description: VPN client for subscriptions with automatic core switching
 CoreShift connects through VLESS and other protocols with the xray,
 sing-box and mihomo cores, switching to the next one when the current
 one stops working. A system service (coreshift.service) runs the cores,
 the TUN interface and DNS protection; the app runs without privileges.
 Members of the coreshift group may use it.
EOF
	for s in postinst prerm postrm; do install -m 755 "$here/deb/$s" "$stage/DEBIAN/$s"; done
	deb="$out/coreshift_${debversion}_$arch.deb"
	dpkg-deb --root-owner-group -Zxz --build "$stage" "$deb" >/dev/null
	rm -rf "$stage/DEBIAN"
	echo "  $deb"
fi

if [ "$make_tar" = 1 ]; then
	step "tar.gz $tag"
	name="coreshift-$tag-linux-$arch"
	tdir="$out/linux-tar/$name"
	rm -rf "$out/linux-tar"
	mkdir -p "$tdir/opt"
	cp -a "$app" "$tdir/opt/coreshift"
	cp "$here/coreshift.service" "$here/dev.coreshift.coreshift.desktop" "$tdir/"
	cp "$root/app/web/icons/Icon-512.png" "$tdir/coreshift.png"
	install -m 755 "$here/install.sh" "$here/uninstall.sh" "$tdir/"
	tar -C "$out/linux-tar" --owner=0 --group=0 -czf "$out/$name.tar.gz" "$name"
	rm -rf "$out/linux-tar"
	echo "  $out/$name.tar.gz"
fi

rm -rf "$stage"
printf '\033[32mDone\033[0m\n'
