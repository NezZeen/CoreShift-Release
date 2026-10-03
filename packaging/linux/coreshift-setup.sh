#!/bin/sh
# Sets CoreShift up on the system after its files are in place, or takes
# it down before they go. The .deb's maintainer scripts and install.sh /
# uninstall.sh call it as root; it is installed as
# /opt/coreshift/coreshift-setup.sh.
#
#   coreshift-setup.sh configure [USER]   group, data directory, service
#   coreshift-setup.sh stop               stop and disable the service
#   coreshift-setup.sh purge              remove settings and subscriptions
#
# USER is added to the coreshift group, whose members may use the daemon;
# by default the user who ran sudo or pkexec.
set -eu

GROUP=coreshift
UNIT=coreshift.service
DATA=/var/lib/coreshift

say() { echo "coreshift: $*"; }

have() { command -v "$1" >/dev/null 2>&1; }

# systemctl does nothing useful without a running systemd (containers,
# chroots); the package still installs.
systemd_up() { [ -d /run/systemd/system ] && have systemctl; }

# installing_user prints who installed CoreShift: the user behind sudo or
# pkexec (software centres), never root.
installing_user() {
	u="${1:-${SUDO_USER:-}}"
	if [ -z "$u" ] && [ -n "${PKEXEC_UID:-}" ]; then
		u=$(getent passwd "$PKEXEC_UID" | cut -d: -f1 || true)
	fi
	if [ -n "$u" ] && [ "$u" != root ] && getent passwd "$u" >/dev/null; then
		echo "$u"
	fi
}

configure() {
	if ! getent group "$GROUP" >/dev/null; then
		groupadd --system "$GROUP"
		say "created group $GROUP"
	fi
	u=$(installing_user "${1:-}")
	if [ -n "$u" ]; then
		if id -nG "$u" | tr ' ' '\n' | grep -qx "$GROUP"; then
			:
		else
			usermod -aG "$GROUP" "$u"
			say "added $u to group $GROUP: sign out and in again before starting CoreShift"
		fi
	else
		say "add the users of CoreShift to group $GROUP: sudo usermod -aG $GROUP <user>"
	fi

	# The daemon sets these itself on every start; doing it here too means
	# the app can reach api.json from the first start on.
	mkdir -p "$DATA"
	chown root:"$GROUP" "$DATA"
	chmod 0710 "$DATA"

	# Files copied by install.sh carry the labels of where they came from.
	if have restorecon; then restorecon -R /opt/coreshift "$DATA" 2>/dev/null || true; fi

	if systemd_up; then
		systemctl daemon-reload
		systemctl enable --quiet "$UNIT"
		# restart, not start: an upgrade replaced the executable.
		systemctl restart "$UNIT" || say "the service did not start: journalctl -u $UNIT"
	fi
	if have update-desktop-database; then update-desktop-database -q 2>/dev/null || true; fi
	if have gtk-update-icon-cache; then
		for d in /usr/share/icons/hicolor /usr/local/share/icons/hicolor; do
			[ -d "$d" ] && gtk-update-icon-cache -q -t "$d" 2>/dev/null || true
		done
	fi
}

stop() {
	# Stopping disconnects and restores DNS.
	if systemd_up; then
		systemctl disable --now "$UNIT" >/dev/null 2>&1 || true
	fi
}

purge() {
	rm -rf "$DATA"
	# The group is left: files elsewhere may still belong to it.
}

case "${1:-}" in
configure) configure "${2:-}" ;;
stop) stop ;;
purge) purge ;;
*)
	echo "usage: $0 {configure [USER]|stop|purge}" >&2
	exit 2
	;;
esac
