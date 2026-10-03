#!/usr/bin/env bash
# Builds CoreShift for Linux on a Linux machine (or CI) of the target
# processor; Flutter cannot cross-build Linux apps. Into dist/:
#
#   coreshift_<version>_<arch>.deb                Debian, Ubuntu, Mint, Pop!_OS       needs dpkg-deb
#   coreshift-<version>-<rel>.<arch>.rpm          Fedora, RHEL/Alma/Rocky, openSUSE   needs rpmbuild
#   coreshift-<version>-<rel>-<arch>.pkg.tar.zst  Arch, Manjaro, EndeavourOS          needs zstd
#   coreshift-<tag>-linux-<arch>.tar.gz           everything else (install.sh)        needs tar, gzip
#
#   packaging/linux/build.sh [--fetch-cores] [--cores DIR] [--out DIR] [--formats deb,rpm,arch,tar]
#
# A format whose tool is missing is skipped with a note. Also needs go,
# flutter with its Linux toolchain (clang cmake ninja-build pkg-config
# libgtk-3-dev) and git. See packaging/linux/README.md.
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
x86_64 | amd64) arch=amd64 flutter_arch=x64 rpm_arch=x86_64 pac_arch=x86_64 ;;
aarch64 | arm64) arch=arm64 flutter_arch=arm64 rpm_arch=aarch64 pac_arch=aarch64 ;;
*)
	echo "unsupported processor $(uname -m)" >&2
	exit 1
	;;
esac

cores="$root/engine/testdata/bin/linux-$arch"
out="$root/dist"
formats="deb,rpm,arch,tar"
fetch=0
while [ $# -gt 0 ]; do
	case "$1" in
	--cores) cores=$(cd "$2" && pwd) && shift ;;
	--out) out="$2" && shift ;;
	--formats) formats="$2" && shift ;;
	--fetch-cores) fetch=1 ;;
	*)
		sed -n '2,27p' "$0" >&2
		exit 2
		;;
	esac
	shift
done
want() { [[ ",$formats," == *",$1,"* ]]; }

step() { printf '\033[36m==> %s\033[0m\n' "$*"; }
skip() { printf '\033[33m--- skipped %s: %s\033[0m\n' "$1" "$2"; }
have() { command -v "$1" >/dev/null 2>&1; }
need() { have "$1" || {
	echo "$1 not found: $2" >&2
	exit 1
}; }

need git 'install git'
need go 'install Go 1.26 or newer'
need flutter 'install Flutter and add its bin to PATH'

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
# Release builds: the version alone. Builds between releases sort after
# the release they follow in every package manager:
#   tar  0.6.5-b130     deb  0.6.5+b130     rpm  0.6.5-1.b130     arch  0.6.5-1.130
tag="$version" debversion="$version" rpmrel=1 pacrel=1
if [ -z "$(git -C "$root" tag --points-at HEAD --list "v$version")" ]; then
	tag="$tag-b$build" debversion="$debversion+b$build" rpmrel="1.b$build" pacrel="1.$build"
fi
if [[ "$commit" == *-dirty ]]; then
	tag="$tag-dirty" debversion="$debversion+dirty" rpmrel="$rpmrel.dirty"
fi

work="$out/linux-work"
bundle="$work/bundle" # the app, the cores and the setup script
rm -rf "$work"
mkdir -p "$bundle" "$out"

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
# Static (no cgo): the same daemon runs on glibc and musl (Alpine).
(cd "$root/engine" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
	-ldflags "-s -w -X $pkg.Version=$version -X $pkg.Build=$build -X $pkg.Commit=$commit" \
	-o "$work/coreshiftd" ./cmd/coreshiftd)

step 'app'
(cd "$root/app" && flutter build linux --release --build-name "$version" --build-number "$build" \
	"--dart-define=CORESHIFT_VERSION=$version" "--dart-define=CORESHIFT_BUILD=$build" "--dart-define=CORESHIFT_COMMIT=$commit")
cp -a "$root/app/build/linux/$flutter_arch/release/bundle/." "$bundle/"

step "cores from $cores"
mkdir -p "$bundle/cores"
for c in xray sing-box mihomo; do install -m 755 "$cores/$c" "$bundle/cores/$c"; done
# Licences that come with the releases, if kept beside them.
find "$cores" -maxdepth 1 -type f \( -iname 'LICENSE*' -o -iname '*.md' \) ! -iname 'README*' -exec cp {} "$bundle/cores/" \;
"$bundle/cores/xray" version >/dev/null && "$bundle/cores/sing-box" version >/dev/null && "$bundle/cores/mihomo" -v >/dev/null || {
	echo "a core does not start: wrong processor or a broken file" >&2
	exit 1
}
install -m 755 "$here/coreshift-setup.sh" "$bundle/coreshift-setup.sh"

# render fills in where the files are for the service and the menu entry.
render() { sed -e "s|@BINDIR@|$1|g" -e "s|@LIBDIR@|$2|g" "$3"; }

# The packages' tree: /usr/lib/coreshift, /usr/bin. Under /usr the files
# get SELinux's and AppArmor's ordinary labels for programs (bin_t, lib_t),
# and nothing executable lives under /var/lib.
root_fs="$work/root"
step 'package tree'
mkdir -p "$root_fs/usr/lib" "$root_fs/usr/bin" "$root_fs/usr/lib/systemd/system" "$root_fs/usr/share/applications" "$root_fs/usr/share/icons"
cp -a "$bundle" "$root_fs/usr/lib/coreshift"
install -m 755 "$work/coreshiftd" "$root_fs/usr/bin/coreshiftd"
ln -s ../lib/coreshift/coreshift "$root_fs/usr/bin/coreshift"
render /usr/bin /usr/lib/coreshift "$here/coreshift.service" >"$root_fs/usr/lib/systemd/system/coreshift.service"
render /usr/bin /usr/lib/coreshift "$here/dev.coreshift.coreshift.desktop" >"$root_fs/usr/share/applications/dev.coreshift.coreshift.desktop"
cp -r "$root/app/linux/icons/hicolor" "$root_fs/usr/share/icons/"
find "$root_fs/usr/share" "$root_fs/usr/lib/systemd" -type d -exec chmod 755 {} + -o -type f -exec chmod 644 {} +
chmod -R go-w "$root_fs"
size_kb=$(du -sk "$root_fs" | cut -f1)

describe="CoreShift connects through VLESS and other protocols with the xray,
sing-box and mihomo cores, switching to the next one when the current one
stops working. A system service (coreshift.service) runs the cores, the
TUN interface and DNS protection; the app runs without privileges.
Members of the coreshift group may use it."

if want deb; then
	if have dpkg-deb; then
		step "deb $debversion"
		d="$work/deb"
		cp -a "$root_fs" "$d"
		mkdir -p "$d/DEBIAN"
		{
			cat <<EOF
Package: coreshift
Version: $debversion
Section: net
Priority: optional
Architecture: $arch
Maintainer: CoreShift <coreshift@localhost>
Installed-Size: $size_kb
Depends: libgtk-3-0 | libgtk-3-0t64, libegl1, libgles2, libx11-6, libxi6, passwd
Recommends: pkexec | policykit-1, libnotify-bin, xdg-utils, desktop-file-utils
Suggests: gnome-shell-extension-appindicator
Homepage: https://github.com/NezZeen/CoreShift-Release
Description: VPN client for subscriptions with automatic core switching
EOF
			sed 's/^/ /' <<<"$describe"
		} >"$d/DEBIAN/control"
		for s in postinst prerm postrm; do install -m 755 "$here/deb/$s" "$d/DEBIAN/$s"; done
		dpkg-deb --root-owner-group -Zxz --build "$d" "$out/coreshift_${debversion}_$arch.deb" >/dev/null
		echo "  $out/coreshift_${debversion}_$arch.deb"
	else
		skip deb 'no dpkg-deb (apt install dpkg-dev)'
	fi
fi

if want rpm; then
	if have rpmbuild; then
		step "rpm $version-$rpmrel"
		rpmbuild -bb --quiet \
			--define "_topdir $work/rpmbuild" --define "_rpmdir $out" --define "_build_name_fmt %%{NAME}-%%{VERSION}-%%{RELEASE}.%%{ARCH}.rpm" \
			--define "stage $root_fs" --define "pkgversion $version" --define "pkgrelease $rpmrel" \
			--target "$rpm_arch" "$here/rpm/coreshift.spec" >/dev/null
		echo "  $out/coreshift-$version-$rpmrel.$rpm_arch.rpm"
	else
		skip rpm 'no rpmbuild (apt install rpm / dnf install rpm-build)'
	fi
fi

if want arch; then
	if have zstd && tar --version 2>/dev/null | grep -q GNU; then
		# The same package makepkg would make from arch/PKGBUILD: .PKGINFO
		# and .INSTALL first, then the files, as root's.
		step "arch $version-$pacrel"
		a="$work/arch"
		cp -a "$root_fs" "$a"
		cp "$here/arch/coreshift.install" "$a/.INSTALL"
		{
			echo "pkgname = coreshift"
			echo "pkgbase = coreshift"
			echo "pkgver = $version-$pacrel"
			echo "pkgdesc = VPN client for subscriptions with automatic core switching"
			echo "url = https://github.com/NezZeen/CoreShift-Release"
			echo "builddate = $(date +%s)"
			echo "packager = CoreShift build.sh"
			echo "size = $((size_kb * 1024))"
			echo "arch = $pac_arch"
			echo "license = LicenseRef-Proprietary"
			for dep in gtk3 libglvnd libx11 libxi systemd; do echo "depend = $dep"; done
			echo "optdepend = polkit: start the service from the app"
			echo "optdepend = libnotify: notifications"
			echo "optdepend = gnome-shell-extension-appindicator: the tray icon in GNOME"
		} >"$a/.PKGINFO"
		pkgfile="$out/coreshift-$version-$pacrel-$pac_arch.pkg.tar.zst"
		(cd "$a" && LC_ALL=C tar --owner=0 --group=0 --numeric-owner -cf - .PKGINFO .INSTALL usr | zstd -q -19 -T0 -f -o "$pkgfile")
		echo "  $pkgfile"
	else
		skip arch 'no zstd or GNU tar (or use arch/PKGBUILD with makepkg on Arch)'
	fi
fi

if want tar; then
	step "tar.gz $tag"
	name="coreshift-$tag-linux-$arch"
	t="$work/tar/$name"
	mkdir -p "$t/lib" "$t/bin" "$t/service/openrc" "$t/service/runit" "$t/share/applications"
	cp -a "$bundle" "$t/lib/coreshift"
	install -m 755 "$work/coreshiftd" "$t/bin/coreshiftd"
	# Templates: install.sh fills in /usr/local.
	cp "$here/coreshift.service" "$t/service/"
	cp "$here/openrc/coreshift" "$t/service/openrc/"
	cp "$here/runit/run" "$here/runit/finish" "$t/service/runit/"
	cp "$here/dev.coreshift.coreshift.desktop" "$t/share/applications/"
	cp -r "$root/app/linux/icons" "$t/share/icons"
	install -m 755 "$here/install.sh" "$here/uninstall.sh" "$t/"
	chmod -R go-w "$t"
	tar -C "$work/tar" --owner=0 --group=0 -czf "$out/$name.tar.gz" "$name"
	echo "  $out/$name.tar.gz"
fi

rm -rf "$work"
printf '\033[32mDone\033[0m\n'
