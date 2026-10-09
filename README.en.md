<div align="center">

🇷🇺 [Русский](README.md) | 🇺🇸 **English**

# CoreShift

[![Download for Windows](https://img.shields.io/badge/Windows-download-0078D4?style=for-the-badge&logo=windows&logoColor=white)](https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift-Setup.exe)
[![Download for Linux](https://img.shields.io/badge/Linux-download-FCC624?style=for-the-badge&logo=linux&logoColor=black)](https://github.com/NezZeen/CoreShift-Release/releases/latest)
[![Download for Android](https://img.shields.io/badge/Android-download%20APK-3DDC84?style=for-the-badge&logo=android&logoColor=white)](https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift.apk)

[![Latest version](https://img.shields.io/github/v/release/NezZeen/CoreShift-Release?label=version&color=6D8CFF)](https://github.com/NezZeen/CoreShift-Release/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/NezZeen/CoreShift-Release/total?label=downloads&color=34D399)](https://github.com/NezZeen/CoreShift-Release/releases)
[![Telegram chat](https://img.shields.io/badge/Telegram-chat-26A5E4?logo=telegram&logoColor=white)](https://t.me/CoreShift_app)

</div>

A VPN client for subscriptions on Windows, Linux and Android. It keeps the connection up by itself: it watches the core, the server and the network and fixes what broke, without the user's help.

Inside are three proxy cores — **Xray, sing-box and mihomo**. If the current core crashes, rejects its config or stops passing traffic, CoreShift switches to the next one, while the tunnel and DNS stay as they are and apps keep their connections.

Builds for Windows, Linux and Android are on the [Releases](https://github.com/NezZeen/CoreShift-Release/releases) page. Chat: [t.me/CoreShift_app](https://t.me/CoreShift_app) (in Russian).

## What the client does

### Subscriptions and servers

- **Formats.** `https://…` subscription links, server lists with VLESS (Reality, XHTTP, gRPC, WS…), VMess, Trojan, Shadowsocks, Hysteria2, TUIC, AnyTLS, WireGuard, Xray JSON (for example from Remnawave) and Clash configs. Each server runs on a core that supports it.
- **Adding.** With the «Добавить в CoreShift» button on a panel's subscription page (a `coreshift://add/<link>` link), from the clipboard (links meant for Happ, v2rayNG, v2RayTun, Hiddify, Clash and sing-box work too), from a QR code, or by hand. CoreShift asks before adding and never shows the key from the link. The system opens CoreShift only for `coreshift://`; other clients' schemes are left to them.
- **Subscription updates** at the interval the panel suggests, or your own. Traffic and expiry are shown in the same units as the panel; a few days before expiry and at 90% traffic a warning with a «Renew» button and a notification appear.
- **Choosing a server.** Pings to every server and «Fastest» in one tap. A server that doesn't answer a plain ping is tested with a real request through a core.
- **«Another server».** When a server stops answering, CoreShift moves to a working server of the subscription and says which one failed.

### A connection that holds

- **Three cores with automatic switching.** Xray → sing-box → mihomo; when the main core is fine again, CoreShift can go back to it by itself.
- **Connectivity check** through the tunnel: one address, the fallbacks only when it fails. A check cut short by a core switch doesn't count as failed.
- **«No network».** CoreShift tells a lost internet connection from a dead server. Without a network it doesn't cycle through cores and servers: it waits, shows «No network» and carries on when the network is back. «Connect» without a network shows «Waiting for network…» and connects by itself.
- **DNS after the phone sleeps.** The tunnel's DNS connections are renewed after sleep and long idle periods, and a failed query is retried once.
- **DPI bypass.** One switch splits the start of encrypted connections so the provider's filters no longer recognise it.

### Routing

- Everything through the VPN except exceptions, or only what you choose.
- «Russian sites direct» with one switch, while Google, YouTube and sites blocked in Russia go through the VPN; a separate switch can send Russian sites hosted abroad through the VPN too. The site databases are built into the app and update themselves; a new copy is accepted only if it passes checks.
- Ad blocking (runetfreedom's list: v2fly, AdGuard DNS filter, Peter Lowe), on by default.
- Ready-made sets for popular services, your own sites (through the VPN, around it, or blocked), internationalized domain names.
- Your own rules such as `geosite:category-ads-all` → block, `geosite:youtube` → through the VPN, `geoip:ru` → direct; categories come from SagerNet, runetfreedom or your own link (`.srs` or a v2ray `geosite.dat`/`geoip.dat`).
- Individual programs on Windows and Linux and apps on Android — through the VPN or around it.
- Proxy mode without TUN when «All apps through the VPN» is off.
- The local network (router, printers, virtual machines) keeps working with the VPN on.

### Leak protection

- DNS is intercepted system-wide (on Windows with NRPT and `strict_route`, on Linux with systemd-resolved, resolvconf, netconfig or `resolv.conf`); every change is written to a rollback journal and undone even after a crash.
- DNS-over-TLS can be blocked. A built-in check shows whether sites see your real DNS.
- With IPv6 off, its traffic doesn't bypass the VPN: the tunnel takes it and refuses it at once, so programs fall back to IPv4 without delays.

### Interface

- Five tabs, the same on desktop and phone: Home, Servers, Rules, Journal, Settings. Fine-tuning lives under «Advanced».
- On Home: your IP and country, speed, traffic for the week, a speed test via speedtest.net through the VPN or without it.
- Journal: repeated core lines are folded («— N more times in 30 s»), errors in red, warnings in yellow, search and filter. The copy for support hides subscription links and UUIDs.
- Dark and light themes, connect at startup, tray on desktop, a notification with the speed on Android. With the screen off, Android polls traffic and connectivity less often and saves battery.
- The «Support» button opens the provider's chat if the panel gives one.

### Updates

- **CoreShift** looks for a new version once a day in the public [NezZeen/CoreShift-Release](https://github.com/NezZeen/CoreShift-Release). The manifest is signed with Ed25519 and checked against built-in keys, the installer against its SHA-256. On Windows the update is installed while the VPN is off and doesn't drop the connection; on Android the system installs it when you confirm; on Linux the app announces the new version and the package manager installs it.
- **Cores** update themselves from their projects' releases. A new core is test-started first; if it doesn't start, the old one stays.

## How it's built

| Folder | What's there |
| --- | --- |
| `engine/` | The Go engine. On desktop it runs as the system service `coreshiftd`, on Android as a library inside the app (`engine/mobile`, gomobile). |
| `app/` | The Flutter interface for Windows, Linux and Android (and a web demo). On desktop it talks to the service over a local HTTP API, on Android it calls the engine directly. |
| `packaging/` | Building and releasing: the Windows installer (Inno Setup), the APK, Linux packages, signed update manifests, publishing. See [packaging/README.md](packaging/README.md) and [packaging/linux/README.md](packaging/linux/README.md) (in Russian). |
| `docs/` | Results of feature and security reviews. |
| `prototype/` | The original HTML mock-up of the interface, rewritten in Flutter in the app. |

### Engine (`engine/internal`)

| Package | Job |
| --- | --- |
| `service` | The heart of the service: connection state, reconnecting, «No network», choosing and switching servers, the journal, the local API and the event stream for the interface. |
| `supervisor` | Runs the cores as separate processes, checks their health and switches to the next one on failure. |
| `core` | Configs for Xray, sing-box and mihomo from one server description. |
| `tunlayer` | The persistent TUN + DNS layer on sing-box (fake-ip), kept apart from the swappable core. |
| `dnsguard` | Points the system's DNS into the tunnel and rolls changes back from a journal. |
| `dnswake` | Renews the tunnel's DNS connections after the device sleeps (Android). |
| `subscription`, `node` | Parsing subscriptions of every format and describing servers. |
| `store` | Settings and subscriptions on disk, written atomically. |
| `ruleset` | The built-in site databases for the rules and their checks on update. |
| `ping` | Server pings and the default-route check («is there a network»). |
| `doh` | DNS over HTTPS for server names the provider's DNS can't resolve. |
| `apps`, `proc` | The list of running programs for the rules, and process control. |
| `coreupdate` | Core updates from their GitHub releases. |
| `selfupdate` | CoreShift's self-update: finding the release, checking the signature and SHA-256, downloading. |
| `fsutil` | Shared file helpers. |

Commands: `engine/cmd/coreshiftd` — the service and its subcommands (installing the service, checking for updates, connecting without the interface); `engine/cmd/coreshift-release` — preparing and signing release files, refreshing the built-in rule databases.

### Interface (`app/lib`)

| Folder | What's there |
| --- | --- |
| `api/` | The service API client, models, the service authenticity check (`proof.dart`), the demo backend. |
| `state/` | App state, turning service errors into readable text, parsing import links, the leak check, hiding data in the journal. |
| `ui/` | Pages (home, servers, rules, journal, settings, cores), shared widgets, theme, QR. |
| `platform/` | Platform differences: tray and window on desktop, the Windows service, the Linux desktop, API access. |

### Key decisions

- **The core is swappable, the tunnel is persistent.** Cores run as separate processes with a SOCKS inbound (with a username and password per start). A separate layer holds TUN and DNS, so switching cores doesn't touch the system's routes or DNS.
- **Separated privileges.** On Windows the service runs as SYSTEM and the interface without privileges; on Linux the service is root in a systemd sandbox; on Android it's a `VpnService`.
- **A protected local API.** The service listens on `127.0.0.1` on a random port and accepts only requests with the token from `api.json` and the right `Host`; the app trusts the service only once it proves it knows the token (HMAC). On Windows only members of the «CoreShift Users» group may use the service, on Linux the `coreshift` group. Request size, timeouts and the number of event streams are limited.
- **Updates are checked by signature**; the signing key never enters the repository.

## Development

You need Go, Flutter and Git. Building for Windows also needs Visual Studio (C++) and Inno Setup 6; for Android, the Android SDK with NDK 28.2 and `gomobile`; for Linux, WSL with Ubuntu 24.04.

Engine:

```
cd engine
go vet ./...
go test ./...
GOOS=linux go build ./... && GOOS=linux go vet ./...
GOOS=android GOARCH=arm64 go vet ./mobile
```

Interface:

```
cd app
flutter analyze
flutter test
flutter run -d windows --dart-define=DEMO=true   # with the demo backend, no service needed
```

Dart code is formatted with `dart format --line-length 160`. The cores (xray, sing-box, mihomo) are not kept in git: the build takes them from `engine/testdata/bin`.

## Building and releasing

The version is the `VERSION` file; the build number is the commit count (users only see the version).

```
powershell -ExecutionPolicy Bypass -File packaging\windows\build.ps1               # Windows installer
powershell -ExecutionPolicy Bypass -File packaging\android\build.ps1               # APK for phones (arm64)
powershell -ExecutionPolicy Bypass -File packaging\android\build.ps1 -Abi x86_64   # APK for the emulator
powershell -ExecutionPolicy Bypass -File packaging\linux\build-wsl.ps1 -Ref main   # .deb, .rpm, Arch, .tar.gz (in WSL)
```

A full release: `packaging\release.ps1 -Version X` builds every platform and signs the manifests, `packaging\publish.ps1 -Version X` publishes the release to [NezZeen/CoreShift-Release](https://github.com/NezZeen/CoreShift-Release). Details are in [packaging/README.md](packaging/README.md).
