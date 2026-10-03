#!/bin/sh
# Removes CoreShift installed by install.sh:
#
#   sudo ./uninstall.sh            keeps settings and subscriptions
#   sudo ./uninstall.sh --purge    removes them too
#
# Stopping the service first disconnects the VPN and restores DNS.
set -eu

if [ "$(id -u)" != 0 ]; then
	echo "run as root: sudo $0" >&2
	exit 1
fi

if [ -x /opt/coreshift/coreshift-setup.sh ]; then
	/opt/coreshift/coreshift-setup.sh stop
else
	systemctl disable --now coreshift.service 2>/dev/null || true
fi

rm -f /etc/systemd/system/coreshift.service
rm -f /usr/local/share/applications/dev.coreshift.coreshift.desktop
rm -f /usr/local/share/icons/hicolor/*/apps/coreshift.png /usr/local/share/icons/hicolor/scalable/apps/coreshift.svg
rm -f /usr/local/bin/coreshift /usr/local/bin/coreshiftd
rm -rf /opt/coreshift
if [ -n "${SUDO_USER:-}" ] && home=$(getent passwd "$SUDO_USER" | cut -d: -f6) && [ -n "$home" ]; then
	# "Автозапуск" of the user who runs this.
	rm -f "$home/.config/autostart/dev.coreshift.coreshift.desktop"
fi
systemctl daemon-reload 2>/dev/null || true
command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database -q /usr/local/share/applications 2>/dev/null || true

if [ "${1:-}" = "--purge" ]; then
	rm -rf /var/lib/coreshift
	echo "CoreShift is removed, with its settings and subscriptions."
else
	echo "CoreShift is removed. Settings and subscriptions stay in /var/lib/coreshift (--purge removes them)."
fi
