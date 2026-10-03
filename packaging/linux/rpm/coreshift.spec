# The CoreShift .rpm for Fedora, RHEL/Alma/Rocky and openSUSE. build.sh
# stages the files and runs:
#
#   rpmbuild -bb --define "stage DIR" --define "pkgversion V" --define "pkgrelease R" \
#            --define "_rpmdir OUT" --target ARCH packaging/linux/rpm/coreshift.spec
#
# Nothing is compiled here: the spec only packs what build.sh built, as
# the .deb does, so both carry the same files.

Name:           coreshift
Version:        %{pkgversion}
Release:        %{pkgrelease}
Summary:        VPN client for subscriptions with automatic core switching
License:        Proprietary
URL:            https://github.com/NezZeen/CoreShift-Release

# Prebuilt Go and Flutter binaries: no debug packages, no stripping or
# rewriting by the distribution's scripts.
%global debug_package %{nil}
%global __os_install_post %{nil}
%define _build_id_links none
# The Flutter bundle carries its own libraries; the rest are listed here,
# by names both Fedora and openSUSE know.
AutoReqProv:    no
Requires:       (gtk3 or libgtk-3-0)
Requires:       (libX11 or libX11-6)
Requires:       (libXi or libXi6)
Requires:       systemd
Requires:       (shadow-utils or shadow)
Recommends:     polkit
Recommends:     libnotify
Recommends:     (gnome-shell-extension-appindicator if gnome-shell)

%description
CoreShift connects through VLESS and other protocols with the xray,
sing-box and mihomo cores, switching to the next one when the current one
stops working. A system service (coreshift.service) runs the cores, the
TUN interface and DNS protection; the app runs without privileges.
Members of the coreshift group may use it.

%install
mkdir -p %{buildroot}
cp -a %{stage}/. %{buildroot}/

%files
/usr/bin/coreshift
/usr/bin/coreshiftd
/usr/lib/coreshift
/usr/lib/systemd/system/coreshift.service
/usr/share/applications/dev.coreshift.coreshift.desktop
/usr/share/icons/hicolor/*/apps/coreshift.*

%post
/usr/lib/coreshift/coreshift-setup.sh configure || :

%preun
# 0: removed, not upgraded. Stopping restores the system's DNS.
if [ "$1" -eq 0 ]; then
	/usr/lib/coreshift/coreshift-setup.sh stop || :
fi

%postun
if [ -d /run/systemd/system ]; then systemctl daemon-reload || :; fi
if [ "$1" -ge 1 ] && [ -d /run/systemd/system ]; then
	# Upgraded: the new daemon takes over.
	systemctl try-restart coreshift.service || :
fi
