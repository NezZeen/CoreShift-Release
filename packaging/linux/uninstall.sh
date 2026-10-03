#!/bin/sh
# Removes CoreShift installed by install.sh:
#
#   sudo ./uninstall.sh            keeps settings and subscriptions
#   sudo ./uninstall.sh --purge    removes them too
#
# Stopping the service first disconnects the VPN and restores DNS.
set -eu

PREFIX=/usr/local
LIBDIR=$PREFIX/lib/coreshift

if [ "$(id -u)" != 0 ]; then
	echo "run as root: sudo $0" >&2
	exit 1
fi

if [ -x "$LIBDIR/coreshift-setup.sh" ]; then
	"$LIBDIR/coreshift-setup.sh" stop
fi

rm -f /etc/systemd/system/coreshift.service /etc/init.d/coreshift
rm -rf /etc/sv/coreshift
rm -f "$PREFIX/share/applications/dev.coreshift.coreshift.desktop"
rm -f "$PREFIX"/share/icons/hicolor/*/apps/coreshift.png "$PREFIX/share/icons/hicolor/scalable/apps/coreshift.svg"
rm -f "$PREFIX/bin/coreshift" "$PREFIX/bin/coreshiftd"
rm -rf "$LIBDIR"
user="${SUDO_USER:-${DOAS_USER:-}}"
if [ -n "$user" ] && home=$(getent passwd "$user" | cut -d: -f6) && [ -n "$home" ]; then
	# "Автозапуск" of the user who runs this.
	rm -f "$home/.config/autostart/dev.coreshift.coreshift.desktop"
fi
if [ -d /run/systemd/system ]; then systemctl daemon-reload 2>/dev/null || true; fi
if command -v update-desktop-database >/dev/null 2>&1; then
	update-desktop-database -q "$PREFIX/share/applications" 2>/dev/null || true
fi

if [ "${1:-}" = "--purge" ]; then
	rm -rf /var/lib/coreshift /var/log/coreshift.log
	echo "CoreShift is removed, with its settings and subscriptions."
else
	echo "CoreShift is removed. Settings and subscriptions stay in /var/lib/coreshift (--purge removes them)."
fi
