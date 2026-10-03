#!/bin/sh
# Sets CoreShift up on the system after its files are in place, or takes
# it down before they go. The packages' scripts (.deb, .rpm, Arch) and
# install.sh / uninstall.sh call it as root; it is installed as
# <libdir>/coreshift-setup.sh.
#
#   coreshift-setup.sh configure [USER]   group, data directory, service
#   coreshift-setup.sh stop               stop and disable the service
#   coreshift-setup.sh purge              remove settings and subscriptions
#
# USER is added to the coreshift group, whose members may use the daemon;
# by default the user who ran sudo, doas or pkexec.
#
# The service is whatever the init system wants: a systemd unit, an OpenRC
# script (/etc/init.d/coreshift) or a runit service (/etc/sv/coreshift),
# installed beforehand by the package or install.sh. Without any of them
# it says how to run the daemon by hand.
set -eu

GROUP=coreshift
DATA=/var/lib/coreshift
here=$(cd "$(dirname "$0")" && pwd)

say() { echo "coreshift: $*"; }

have() { command -v "$1" >/dev/null 2>&1; }

# init_system names the running init system, as coreshiftd does
# (engine/cmd/coreshiftd/initsys.go).
init_system() {
	if [ -d /run/systemd/system ]; then
		echo systemd
	elif [ -d /run/openrc ]; then
		echo openrc
	elif [ -d /run/runit ] || [ -d /etc/runit/runsvdir ] || { [ -d /var/service ] && have sv; }; then
		echo runit
	else
		echo none
	fi
}

# runit_dir is where enabled runit services are linked: Void uses
# /var/service, Artix /run/runit/service (and /etc/runit/runsvdir/default).
runit_dir() {
	for d in /var/service /run/runit/service /etc/runit/runsvdir/default /service; do
		if [ -d "$d" ]; then
			echo "$d"
			return
		fi
	done
}

# installing_user prints who installed CoreShift: the user behind sudo,
# doas or pkexec (software centres), never root.
installing_user() {
	u="${1:-${SUDO_USER:-${DOAS_USER:-}}}"
	if [ -z "$u" ] && [ -n "${PKEXEC_UID:-}" ]; then
		u=$(getent passwd "$PKEXEC_UID" | cut -d: -f1 || true)
	fi
	if [ -n "$u" ] && [ "$u" != root ] && getent passwd "$u" >/dev/null 2>&1; then
		echo "$u"
	fi
}

add_group() {
	if getent group "$GROUP" >/dev/null 2>&1; then
		return
	fi
	if have groupadd; then
		groupadd --system "$GROUP"
	else
		addgroup -S "$GROUP" # BusyBox (Alpine)
	fi
	say "created group $GROUP"
}

add_member() {
	if id -nG "$1" | tr ' ' '\n' | grep -qx "$GROUP"; then
		return
	fi
	if have usermod; then
		usermod -aG "$GROUP" "$1"
	else
		addgroup "$1" "$GROUP" # BusyBox
	fi
	say "added $1 to group $GROUP: sign out and in again before starting CoreShift"
}

# selinux_labels gives copied files the labels of where they are now, on
# Fedora, RHEL and others with SELinux; rpm does it for its own files.
selinux_labels() {
	if have restorecon && have selinuxenabled && selinuxenabled; then
		restorecon -R "$here" "$DATA" 2>/dev/null || true
		for f in /usr/bin/coreshiftd /usr/local/bin/coreshiftd /etc/systemd/system/coreshift.service \
			/usr/lib/systemd/system/coreshift.service /etc/init.d/coreshift; do
			[ -e "$f" ] && restorecon "$f" 2>/dev/null || true
		done
	fi
}

start_service() {
	case "$(init_system)" in
	systemd)
		systemctl daemon-reload
		systemctl enable --quiet coreshift.service
		# restart, not start: an upgrade replaced the executable.
		systemctl restart coreshift.service || say "the service did not start: journalctl -u coreshift"
		;;
	openrc)
		rc-update add coreshift default >/dev/null
		rc-service coreshift restart || say "the service did not start: see /var/log/coreshift.log"
		;;
	runit)
		d=$(runit_dir)
		if [ -n "$d" ] && [ -d /etc/sv/coreshift ]; then
			ln -sfn /etc/sv/coreshift "$d/coreshift"
			# runsv picks it up within seconds; restart an older one.
			sleep 6
			sv restart coreshift >/dev/null 2>&1 || true
		else
			say "link /etc/sv/coreshift into your runit service directory to start the service"
		fi
		;;
	*)
		say "no systemd, OpenRC or runit found. Start the daemon as root from your init system:"
		say "  coreshiftd dns recover -journal $DATA/dnsguard.json"
		say "  coreshiftd service run -data-dir $DATA -cores-dir $here/cores"
		;;
	esac
}

configure() {
	add_group
	u=$(installing_user "${1:-}")
	if [ -n "$u" ]; then
		add_member "$u"
	else
		say "add the users of CoreShift to group $GROUP: usermod -aG $GROUP <user>"
	fi

	# The daemon sets these itself on every start; doing it here too means
	# the app can reach api.json from the first start on.
	mkdir -p "$DATA"
	chown root:"$GROUP" "$DATA"
	chmod 0710 "$DATA"

	selinux_labels
	start_service

	if have update-desktop-database; then update-desktop-database -q 2>/dev/null || true; fi
	for d in /usr/share/icons/hicolor /usr/local/share/icons/hicolor; do
		if [ -d "$d" ] && have gtk-update-icon-cache; then gtk-update-icon-cache -q -t "$d" 2>/dev/null || true; fi
	done
}

stop() {
	# Stopping disconnects and restores DNS.
	case "$(init_system)" in
	systemd) systemctl disable --now coreshift.service >/dev/null 2>&1 || true ;;
	openrc)
		rc-service coreshift stop >/dev/null 2>&1 || true
		rc-update del coreshift default >/dev/null 2>&1 || true
		;;
	runit)
		sv stop coreshift >/dev/null 2>&1 || true
		d=$(runit_dir)
		[ -n "$d" ] && rm -f "$d/coreshift"
		;;
	esac
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
