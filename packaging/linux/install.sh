#!/bin/sh
# Installs CoreShift from the .tar.gz on any Linux: distributions without
# a CoreShift package, or with OpenRC or runit instead of systemd. Run from
# the unpacked folder:
#
#   sudo ./install.sh [--daemon-only] [USER]
#
# It finds the init system (systemd, OpenRC, runit) and installs the
# service for it; the daemon finds the DNS stack (systemd-resolved,
# NetworkManager, resolvconf/openresolv, netconfig or a plain
# /etc/resolv.conf) by itself on every connection. On musl systems
# (Alpine) only the daemon is installed: the app needs glibc.
#
# Files go under /usr/local (lib/coreshift, bin/coreshiftd), the service
# into /etc. USER (by default the one who ran sudo or doas) is added to the
# coreshift group, whose members may use the daemon. Running it again over
# an installed copy updates it; settings and subscriptions in
# /var/lib/coreshift stay.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
PREFIX=/usr/local
LIBDIR=$PREFIX/lib/coreshift
BINDIR=$PREFIX/bin
daemon_only=0
user=""
for a in "$@"; do
	case "$a" in
	--daemon-only) daemon_only=1 ;;
	-*)
		echo "usage: $0 [--daemon-only] [USER]" >&2
		exit 2
		;;
	*) user="$a" ;;
	esac
done

say() { echo "coreshift: $*"; }
have() { command -v "$1" >/dev/null 2>&1; }

if [ "$(id -u)" != 0 ]; then
	echo "run as root: sudo $0" >&2
	exit 1
fi
packaged=0
if have dpkg-query && dpkg-query -W -f='${Status}' coreshift 2>/dev/null | grep -q 'install ok installed'; then
	packaged=1
elif have rpm && rpm -q coreshift >/dev/null 2>&1; then
	packaged=1
elif have pacman && pacman -Q coreshift >/dev/null 2>&1; then
	packaged=1
fi
if [ "$packaged" = 1 ]; then
	echo "CoreShift is installed from a package here; update it with a new package" >&2
	exit 1
fi

arch=$(uname -m)
case "$arch" in x86_64) arch=amd64 ;; aarch64) arch=arm64 ;; esac
if ! "$here/bin/coreshiftd" version >/dev/null 2>&1; then
	echo "this archive is not for this processor ($arch)" >&2
	exit 1
fi

# musl (Alpine, Void-musl): Flutter's Linux embedder needs glibc.
musl=0
if ls /lib/ld-musl-* >/dev/null 2>&1; then musl=1; fi
if [ "$musl" = 1 ] && [ "$daemon_only" = 0 ]; then
	say "musl system: installing the daemon only, the CoreShift app needs glibc (see README)"
	daemon_only=1
fi

# init_system as in coreshift-setup.sh.
if [ -d /run/systemd/system ]; then
	init=systemd
elif [ -d /run/openrc ]; then
	init=openrc
elif [ -d /run/runit ] || [ -d /etc/runit/runsvdir ] || { [ -d /var/service ] && have sv; }; then
	init=runit
else
	init=none
fi
say "init system: $init"

render() { sed -e "s|@BINDIR@|$BINDIR|g" -e "s|@LIBDIR@|$LIBDIR|g" "$1"; }

# The daemon stops first: it restores DNS, and the files can be replaced.
if [ -x "$LIBDIR/coreshift-setup.sh" ]; then "$LIBDIR/coreshift-setup.sh" stop; fi

# Replace the files whole: copy aside, then swap. root runs them, so they
# are root's and nobody else may change them, whoever unpacked the archive.
rm -rf "$LIBDIR.new" "$LIBDIR.old"
mkdir -p "$(dirname "$LIBDIR")" "$BINDIR"
cp -a "$here/lib/coreshift" "$LIBDIR.new"
if [ "$daemon_only" = 1 ]; then
	# Keep the cores and the setup script, drop the app.
	find "$LIBDIR.new" -mindepth 1 -maxdepth 1 ! -name cores ! -name coreshift-setup.sh -exec rm -rf {} +
fi
chown -R root:root "$LIBDIR.new"
chmod -R go-w "$LIBDIR.new"
if [ -d "$LIBDIR" ]; then mv "$LIBDIR" "$LIBDIR.old"; fi
mv "$LIBDIR.new" "$LIBDIR"
rm -rf "$LIBDIR.old"
install -m 755 "$here/bin/coreshiftd" "$BINDIR/coreshiftd.new"
mv "$BINDIR/coreshiftd.new" "$BINDIR/coreshiftd"

case "$init" in
systemd)
	render "$here/service/coreshift.service" >/etc/systemd/system/coreshift.service
	chmod 644 /etc/systemd/system/coreshift.service
	;;
openrc)
	render "$here/service/openrc/coreshift" >/etc/init.d/coreshift
	chmod 755 /etc/init.d/coreshift
	;;
runit)
	mkdir -p /etc/sv/coreshift
	render "$here/service/runit/run" >/etc/sv/coreshift/run
	cp "$here/service/runit/finish" /etc/sv/coreshift/finish
	chmod 755 /etc/sv/coreshift/run /etc/sv/coreshift/finish
	;;
esac

if [ "$daemon_only" = 0 ]; then
	ln -sf "$LIBDIR/coreshift" "$BINDIR/coreshift"
	mkdir -p "$PREFIX/share/applications"
	render "$here/share/applications/dev.coreshift.coreshift.desktop" >"$PREFIX/share/applications/dev.coreshift.coreshift.desktop"
	chmod 644 "$PREFIX/share/applications/dev.coreshift.coreshift.desktop"
	(cd "$here/share" && find icons -type f) | while read -r f; do
		install -D -m 644 "$here/share/$f" "$PREFIX/share/$f"
	done
fi

"$LIBDIR/coreshift-setup.sh" configure "$user"

# What manages DNS here, for the record: the daemon decides on every
# connection anyway.
if [ -L /etc/resolv.conf ]; then
	say "/etc/resolv.conf links to $(readlink /etc/resolv.conf)"
fi
if have firewall-cmd && [ -d /run/firewalld ]; then
	say "firewalld runs: the TUN interface goes into its trusted zone while connected"
fi

# The app's libraries: unlike a package, the archive cannot pull them in.
# EGL and GLES are opened at run time, so ldd does not show them missing;
# without them the app exits at once and nothing appears.
missing=""
if [ "$daemon_only" = 0 ] && have ldconfig; then
	libs=$(ldconfig -p 2>/dev/null)
	for l in libgtk-3.so.0 libEGL.so.1 libGLESv2.so.2 libX11.so.6 libXi.so.6; do
		case "$libs" in
		*"$l "*) ;;
		*) missing="$missing $l" ;;
		esac
	done
fi

say "CoreShift $("$BINDIR/coreshiftd" version | cut -d' ' -f2-) is installed."
if [ -n "$missing" ]; then
	say "the app cannot start: missing$missing. Install them, e.g."
	say "  Debian, Ubuntu: apt install libgtk-3-0 libegl1 libgles2 libx11-6 libxi6"
	say "  Fedora: dnf install gtk3 libglvnd-egl libglvnd-gles    Arch: pacman -S gtk3 libglvnd"
fi
if [ "$daemon_only" = 0 ]; then
	say "sign out and in again (for the group), then open CoreShift from the applications menu."
fi
