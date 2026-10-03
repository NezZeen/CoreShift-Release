#!/bin/sh
# Installs CoreShift from the .tar.gz, for distributions without .deb
# (Fedora, Arch, openSUSE...). Run from the unpacked folder:
#
#   sudo ./install.sh [USER]
#
# USER (by default the one who ran sudo) is added to the coreshift group,
# whose members may use the daemon. Running it again over an installed
# copy updates it; settings and subscriptions in /var/lib/coreshift stay.
set -eu

here=$(cd "$(dirname "$0")" && pwd)

if [ "$(id -u)" != 0 ]; then
	echo "run as root: sudo $0" >&2
	exit 1
fi
if command -v dpkg-query >/dev/null 2>&1 && dpkg-query -W -f '${Status}' coreshift 2>/dev/null | grep -q 'install ok installed'; then
	echo "CoreShift is installed from the .deb here; update it with a new .deb" >&2
	exit 1
fi
if ! command -v systemctl >/dev/null 2>&1; then
	echo "CoreShift needs systemd to run its service" >&2
	exit 1
fi

# Replace the files whole: copy aside, then swap. root runs them, so they
# are root's and nobody else may change them, whoever unpacked the archive.
rm -rf /opt/coreshift.new /opt/coreshift.old
cp -a "$here/opt/coreshift" /opt/coreshift.new
chown -R root:root /opt/coreshift.new
chmod -R go-w /opt/coreshift.new
if [ -d /opt/coreshift ]; then
	mv /opt/coreshift /opt/coreshift.old
fi
mv /opt/coreshift.new /opt/coreshift
rm -rf /opt/coreshift.old

install -D -m 644 "$here/coreshift.service" /etc/systemd/system/coreshift.service
install -D -m 644 "$here/dev.coreshift.coreshift.desktop" /usr/local/share/applications/dev.coreshift.coreshift.desktop
for f in "$here"/icons/*/apps/coreshift.*; do
	size=$(basename "$(dirname "$(dirname "$f")")")
	install -D -m 644 "$f" "/usr/local/share/icons/hicolor/$size/apps/$(basename "$f")"
done
mkdir -p /usr/local/bin
ln -sf /opt/coreshift/coreshift /usr/local/bin/coreshift
ln -sf /opt/coreshift/coreshiftd /usr/local/bin/coreshiftd

/opt/coreshift/coreshift-setup.sh configure "${1:-}"
echo "CoreShift $(/opt/coreshift/coreshiftd version | cut -d' ' -f2-) is installed. Open it from the applications menu."
