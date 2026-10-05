# CoreShift для Linux

Сборка, установка и проверка CoreShift на Linux: Debian и Ubuntu, Fedora, RHEL и openSUSE, Arch, а также любые системы с systemd, OpenRC или runit.

## Пакеты

| Система | Файл | Как поставить | Чем собирается |
| --- | --- | --- | --- |
| Debian, Ubuntu, Mint, Pop!_OS | `coreshift_<версия>_amd64.deb` | `sudo apt install ./coreshift_*.deb` | `dpkg-deb` |
| Fedora, RHEL/Alma/Rocky, openSUSE | `coreshift-<версия>-1.x86_64.rpm` | `sudo dnf install ./coreshift-*.rpm`, `sudo zypper in ./coreshift-*.rpm` | `rpmbuild` (пакет `rpm` / `rpm-build`) |
| Arch, Manjaro, EndeavourOS | `coreshift-<версия>-1-x86_64.pkg.tar.zst` | `sudo pacman -U coreshift-*.pkg.tar.zst` | `zstd` и GNU tar; на Arch можно и `arch/PKGBUILD` с `makepkg` |
| Всё остальное, а также OpenRC и runit | `coreshift-<версия>-linux-amd64.tar.gz` | распаковать, затем `sudo ./install.sh` | `tar`, `gzip` |

Для arm64 те же файлы с `arm64` или `aarch64` в имени. В релизе файлы лежат под постоянными именами: `CoreShift-amd64.deb`, `CoreShift-x86_64.rpm`, `CoreShift-x86_64.pkg.tar.zst`, `CoreShift-linux-amd64.tar.gz` (см. [packaging/README.md](../README.md)).

Почему `.rpm` собирается через `rpmbuild` со spec-файлом (`rpm/coreshift.spec`): он есть в репозиториях Ubuntu и Debian (пакет `rpm`), не тянет новых зависимостей и не требует Go-инструментов вроде nfpm. Spec ничего не компилирует, а только упаковывает то же дерево файлов, что `.deb`. Пакет для Arch `build.sh` собирает сам: это `.PKGINFO`, `.INSTALL` и файлы, сжатые zstd, то есть ровно то, что сделал бы `makepkg`. `makepkg` для этого не нужен, его на Ubuntu нет.

## Как устроено

| Что | Где (пакеты / `install.sh`) | Права |
| --- | --- | --- |
| Приложение (Flutter) | `/usr/lib/coreshift/coreshift`, ярлык `/usr/bin/coreshift` / `/usr/local/...` | пользователь |
| Служба `coreshiftd` | `/usr/bin/coreshiftd` / `/usr/local/bin/coreshiftd` | root в песочнице systemd (см. «Ограничения службы») |
| Ядра xray, sing-box, mihomo | `/usr/lib/coreshift/cores` / `/usr/local/lib/coreshift/cores` | root; обновляются сами |
| Настройки, подписки, журнал DNS | `/var/lib/coreshift` | root (только данные, ничего исполняемого) |

- **Служба работает с загрузки системы.** VPN включается кнопкой и **остаётся включённым, пока его не выключат кнопкой**, даже если окно закрыто. С включённым «Автозапуском» CoreShift запускается при входе в сеанс (`~/.config/autostart/dev.coreshift.coreshift.desktop`), и служба, как на Windows, сразу подключает выбранный сервер. «Выход» в меню трея, как и на Windows, выключает VPN.
- **Доступ к службе.** Приложение говорит со службой по `127.0.0.1` на порту, который система выбирает при каждом запуске службы, с токеном из `/var/lib/coreshift/api.json`. Прежде чем отправить что-то ещё, приложение проверяет, что ответивший знает токен. Читать токен может только группа `coreshift`:

  ```
  /var/lib/coreshift            root:coreshift 0710   члены группы открывают файлы по имени, список не видят
  /var/lib/coreshift/api.json   root:coreshift 0640   адрес и токен
  остальное                     root 0700 / 0600      подписки, настройки, конфиги ядер, журнал DNS
  ```

  Пакет создаёт группу и добавляет в неё того, кто его ставил (через `sudo`, `doas` или центр приложений). Других пользователей добавляют командой `sudo usermod -aG coreshift <имя>`, после чего нужно **выйти из системы и войти снова**. Без группы приложение показывает эту же подсказку. При каждом старте служба проверяет каталог (`SecureDataDir`): ссылка или файл на месте каталога удаляется, всё чужое возвращается root или удаляется.
- **Обновления CoreShift** ставит пакетный менеджер, сама служба ничего не устанавливает. Пакет при обновлении перезапускает службу; если VPN был включён, служба подключается снова (файл `/var/lib/coreshift/resume`, только в той же загрузке системы и не позже чем через 10 минут). После сбоя службы и после перезагрузки VPN остаётся выключенным, пока его не включат или не сработает «Автозапуск». Раз в день она читает подписанный `latest-linux.json` из публичного репозитория `NezZeen/CoreShift-Release` (токена в Linux-сборке нет) и проверяет подпись теми же ключами, что на Windows. Если вышла новая версия, приложение пишет «Доступна новая версия X», а кнопка «Скачать» открывает страницу релиза. Источник меняется настройкой `app_update.source`: `github-public:OWNER/REPO` или папка. Ядра обновляются сами, как на Windows.

### Ограничения службы

Юнит systemd (`coreshift.service`) запускает службу, ядра, TUN-слой и программы DNS (`resolvectl`, `resolvconf`, `netconfig`, `nmcli`, `firewall-cmd`, `restorecon`) в одной песочнице. `systemd-analyze security coreshift` даёт 3.2 «OK» (до октября 2026 было 6.7 «MEDIUM»).

- **Capabilities** (`CapabilityBoundingSet`): только `CAP_NET_ADMIN` (TUN, маршруты, правила, DNS интерфейса в resolved), `CAP_NET_RAW` (привязка сокетов к физическому интерфейсу, ping), `CAP_SYS_PTRACE` и `CAP_DAC_READ_SEARCH` (`/proc/<pid>/exe` и `fd` чужих процессов для правил по приложениям), `CAP_CHOWN` и `CAP_FOWNER` (права на `/var/lib/coreshift`, `SecureDataDir`). Без `CAP_DAC_OVERRIDE`, `CAP_KILL` и `CAP_NET_BIND_SERVICE`: все файлы службы принадлежат root, сигналы она шлёт только своим процессам, порты ниже 1024 не открывает.
- **Файлы** (`ProtectSystem=strict`): всё только для чтения, кроме `/var/lib/coreshift`, каталога ядер (они обновляются сами), `/etc` (`/etc/resolv.conf` заменяется атомарно, через временный файл рядом), `/run/resolvconf`, `/run/netconfig` и `/var/adm/netconfig`, если они есть, и `/proc/sys/net/ipv4/conf` (sing-box ставит `rp_filter` своему интерфейсу). `/home` только для чтения, `/tmp` свой. Каталог `/var/lib/coreshift` юнит создаёт сам (`ExecStartPre=+mkdir`), если его удалили.
- **Остальное**: из устройств только `/dev/net/tun`; ядро, его модули, журнал, часы, имя хоста и cgroups не трогаются; сокеты только `AF_UNIX`, `AF_INET`, `AF_INET6`, `AF_NETLINK`; без новых пространств имён, исполняемой записываемой памяти (`MemoryDenyWriteExecute`) и чужих ABI; системные вызовы — `@system-service`.

Проверено в WSL: подключение с каждым ядром (xray, sing-box, mihomo) через TUN, DNS и откат после `kill -9` на Ubuntu 24.04; `dns apply`/`revert` в той же песочнице на Ubuntu (resolved, файл), Fedora 44 (файл и resolved, firewalld, NetworkManager), openSUSE Tumbleweed (netconfig, файл), Debian 13 и Arch (файл). resolvconf Debian и openresolv в октябре 2026 с новым юнитом не проверялись: в WSL их не было.

### Системы инициализации

| Init | Что ставится | Как работает восстановление DNS после сбоя |
| --- | --- | --- |
| systemd | юнит `coreshift.service` (`Restart=on-failure`) | `ExecStartPre=coreshiftd dns recover` перед каждым стартом, при загрузке тоже, затем ещё раз сама служба |
| OpenRC (Alpine, Gentoo, Artix) | `/etc/init.d/coreshift` с `supervise-daemon` | `start_pre` с `dns recover`; после перезапуска упавшей службы откат делает сама служба |
| runit (Void, Artix) | `/etc/sv/coreshift/run` и `finish` | `run` выполняет `dns recover` перед каждым запуском |
| другое | ничего | `install.sh` пишет, какие две команды запускать от root при загрузке |

Пакеты `.deb`, `.rpm` и для Arch ставят только юнит systemd. `install.sh` сам определяет init-систему и ставит нужный файл. `coreshiftd service start|stop|status` обращается к той init-системе, которая запущена; через неё же работает кнопка «Запустить службу» (`pkexec coreshiftd service start`).

### DNS

Служба определяет способ настройки DNS при каждом подключении:

| Что управляет DNS | Как распознаётся | Что делает CoreShift |
| --- | --- | --- |
| systemd-resolved (Ubuntu, Fedora; NetworkManager поверх него) | resolved запущен, `/etc/resolv.conf` указывает на заглушку `127.0.0.53` | интерфейсу `coreshift` задаются DNS туннеля и домен `~.` (`resolvectl`) |
| resolvconf (Debian) или openresolv (Arch, Void, Alpine, Gentoo) | настоящий `resolvconf` (не `resolvectl`), в шапке файла «generated by resolvconf» или ссылка в `/run/resolvconf` | запись `tun.coreshift`; у openresolv эксклюзивная (`-x`) |
| netconfig (openSUSE) | `netconfig` есть, в шапке файла «netconfig» | свой сервис для netconfig (`netconfig modify -s coreshift`). Если netconfig не поставил сервер туннеля первым (с NetworkManager политика `auto` берёт только его данные, с wicked туннель может оказаться вторым), служба пишет `/etc/resolv.conf` сама, как в последней строке |
| NetworkManager пишет файл сам, обычный файл или ссылка | всё остальное | служба временно пишет свой `/etc/resolv.conf` и каждые 5 с возвращает его, если файл перезаписали |

Если запущен NetworkManager, интерфейс `coreshift` помечается как неуправляемый. Если `/etc/resolv.conf` — ссылка в незнакомое место, служба его не трогает. Это не утечка: sing-box перехватывает в туннеле **любой** DNS-пакет на порт 53, к какому бы серверу он ни шёл. Каждое изменение сначала записывается в журнал. Перед откатом журнал проверяется: служба пишет только в `/etc/resolv.conf`, восстанавливает только ссылки на файлы resolved, NetworkManager, resolvconf, netconfig или WSL, а `resolvectl`, `resolvconf`, `netconfig` и `firewall-cmd` вызывает только с допустимым именем интерфейса.

### Межсетевой экран, SELinux, AppArmor

- **firewalld (Fedora, RHEL, openSUSE).** Пока VPN включён, интерфейс `coreshift` переводится в зону `trusted` (только в runtime, `--change-interface`), а при отключении возвращается обратно. Без этого зона по умолчанию отклоняет соединения, которые TUN-слой отвечает системе. Перезагрузка firewalld или ПК сбрасывает это и так.
- **nftables/iptables со своими правилами** (политика DROP на input): нужно разрешить вход с интерфейса `coreshift`, например `nft add rule inet filter input iifname "coreshift" accept`.
- **Локальная сеть.** Маршруты sing-box (`auto_route`, `strict_route`) отправляют в TUN весь трафик, и на Linux это касалось бы и ответов на входящие соединения: SSH, общие папки, хост виртуальной машины. Поэтому диапазоны локальных сетей (`10/8`, `172.16/12`, `192.168/16`, `169.254/16`, `fd00::/8`, `fe80::/10`, мультикаст) исключены из маршрутов TUN (`route_exclude_address`). Они и так шли напрямую. Исключение — DNS-серверы системы (роутер, хост WSL): их адреса остаются в маршрутах TUN, поэтому запрос к ним на порт 53 перехватывается, даже если программа обращается к ним сама, а остальной трафик к ним всё равно идёт напрямую. Мимо туннеля идёт только DNS к другим серверам в локальной сети, которых система не использует.
- **SELinux (Fedora, RHEL) в режиме enforcing.** Программы лежат в `/usr/bin` и `/usr/lib/coreshift` и получают обычные метки `bin_t` и `lib_t`. Служба запускается из `/usr/bin/coreshiftd` (`bin_t`), поэтому systemd запускает её в домене `unconfined_service_t`. В `/var/lib` нет ничего исполняемого. `rpm` ставит метки сам. Обновлённые ядра пишутся во временный файл в том же каталоге ядер и получают ту же метку `lib_t`. Если служба пишет или возвращает `/etc/resolv.conf` (NetworkManager без resolved, как в RHEL, Alma и Rocky), она вызывает `restorecon /etc/resolv.conf`: иначе файл получил бы метку `etc_t` вместо `net_conf_t`, и NetworkManager не смог бы его заменить. После `install.sh` (`/usr/local`) setup-скрипт сам выполняет `restorecon`. Если служба не стартует, проверьте `ausearch -m avc -ts recent` и при необходимости выполните `sudo restorecon -Rv /usr/local/lib/coreshift /usr/local/bin/coreshiftd /etc/systemd/system/coreshift.service`.
- **AppArmor (Ubuntu, Debian, openSUSE).** Профиля у CoreShift нет, обе программы работают без ограничений (unconfined). Ограничение user namespaces в Ubuntu 24.04 Flutter-приложение не затрагивает.

### Окружения рабочего стола

- **Трей** — StatusNotifierItem. Он сразу есть в KDE, XFCE, Cinnamon, MATE, Budgie и в GNOME у Ubuntu (расширение AppIndicator включено). В «чистом» GNOME (Fedora, Debian, Arch) нужно расширение [AppIndicator](https://extensions.gnome.org/extension/615/appindicator-support/). Меню значка открывается правой кнопкой, первый пункт — «Открыть CoreShift».
- **Без трея** (служба `org.kde.StatusNotifierWatcher` на шине сеанса не найдена) крестик сворачивает окно, а не прячет его, и `--tray` при автозапуске открывает окно. Иначе CoreShift пропал бы из виду насовсем.
- **Wayland и X11.** Работает в обоих: GTK сам выбирает бэкенд. Своё оформление окна рисует приложение, системный заголовок скрыт.
- **Ярлык и ссылки `coreshift://`**: `dev.coreshift.coreshift.desktop` с `MimeType=x-scheme-handler/coreshift`. Пакеты обновляют базу (`update-desktop-database`). Если браузер всё равно не открывает CoreShift, выполните `xdg-mime default dev.coreshift.coreshift.desktop x-scheme-handler/coreshift`. Повторный запуск передаёт ссылку уже открытому окну (GApplication, D-Bus сеанса) и закрывается.
- **Автозапуск** — XDG autostart (`~/.config/autostart`). Его понимают все перечисленные окружения.

### musl (Alpine)

Служба собирается статически (`CGO_ENABLED=0`), а ядра xray, sing-box и mihomo тоже статические, поэтому служба работает и на musl. Встраиваемый движок Flutter для Linux собран под glibc, и `gcompat` для GTK и EGL этого не покрывает. Поэтому на Alpine поддерживается **только служба**: `install.sh` на musl-системе ставит её без приложения (`--daemon-only`). Управлять ею без приложения можно через API (`curl` с токеном) или командами `coreshiftd vpn <файл>` и `coreshiftd connect`.

### Архитектуры

amd64 и arm64. Служба и ядра собираются для обеих где угодно (`GOARCH=arm64`). Приложение Flutter собирается только на машине той же архитектуры, поэтому arm64 собирается на arm64: Raspberry Pi 5, сервер Ampere или раннер GitHub `ubuntu-24.04-arm`. Пример конвейера — [ci/linux-packages.yml](ci/linux-packages.yml): он не подключён, его надо скопировать в `.github/workflows/`.

## Сборка

Собирать нужно на Linux той же архитектуры. С Windows это делается через WSL: `packaging\linux\build-wsl.ps1 -Ref <тег или ветка>`, и так же собирает `release.ps1`.

1. Поставить инструменты (Ubuntu 24.04):

   ```
   sudo apt install git curl unzip xz-utils clang cmake ninja-build pkg-config \
       libgtk-3-dev liblzma-dev libstdc++-12-dev libx11-dev libxi-dev dpkg-dev rpm zstd
   ```

   Go 1.26+ взять с https://go.dev/dl/, Flutter — по https://docs.flutter.dev/get-started/install/linux (та же версия, что на Windows). Проверка: `flutter doctor`.

2. Проверить код (от root в Linux выполняются и тесты, которые без root пропускаются):

   ```
   cd engine && go vet ./... && go test ./... && cd ..
   cd app && flutter analyze && flutter test && cd ..
   ```

3. Собрать всё сразу; `--fetch-cores` скачивает последние ядра в `engine/testdata/bin/linux-amd64` и проверяет SHA-256:

   ```
   packaging/linux/build.sh --fetch-cores               # или --formats deb,tar
   ```

   Результат будет в `dist/`. Если для какого-то формата нет инструмента, этот формат пропускается с пометкой.

## Матрица проверки

Проверено 3 октября 2026 года на сборке `0.6.7+b122` в WSL2 (systemd, WSLg). Отдельно — в ВМ Hyper-V с KDE Plasma (Ubuntu 24.04). Тестовый сервер — локальный shadowsocks на `127.0.0.1`; настоящие подписки не использовались. Что проверялось везде: установка, автозапуск службы, права на `api.json`, подключение, DNS через туннель, HTTPS по имени через туннель, отключение с восстановлением DNS, `kill -9` службы с восстановлением, удаление и очистка.

| Система | Пакет | Init | DNS-стек | Результат |
| --- | --- | --- | --- | --- |
| Ubuntu 24.04 | .deb | systemd | файл (ссылка WSL) | всё ок, приложение и ссылки `coreshift://` тоже |
| Ubuntu 24.04 | .deb | systemd | systemd-resolved (заглушка) | ок |
| Debian 13 | .deb | systemd | файл (ссылка WSL) | ок |
| Debian 13 | .deb | systemd | resolvconf (Debian) | ок |
| Debian 13 | .tar.gz + install.sh | systemd | файл | ок, `/usr/local` |
| Fedora 44 | .rpm | systemd | файл + firewalld | ок, интерфейс переходит в зону `trusted` и обратно |
| Arch | .pkg.tar.zst | systemd | файл / openresolv | ок, у openresolv эксклюзивная запись |
| openSUSE Tumbleweed | .rpm | systemd | файл / netconfig | ок; у netconfig сервер туннеля стоит вторым, DNS всё равно идёт в туннель |
| Ubuntu 24.04 KDE Plasma 5.27 (ВМ Hyper-V) | .deb | systemd | NetworkManager + resolved | ок на `0.6.8+b148`, X11 и Wayland: группа после входа, меню Plasma, трей (меню по правой кнопке), закрытие в трей, второй запуск, `coreshift://`, автозапуск с подключением, `pkexec`, DNS туннеля после `nmcli connection up`, SSH с хоста при включённом VPN. Левая кнопка по значку трея ничего не делает (nativeapi) |
| Alpine | .tar.gz `--daemon-only` | OpenRC | openresolv | не проверено: нет системы |
| Void | .tar.gz | runit | openresolv | не проверено: нет системы |

Что проверить на каждой системе:

1. Установка: служба `enabled`/`active` (OpenRC: `rc-service coreshift status`, runit: `sv status coreshift`); пользователь в группе `coreshift` после повторного входа; `stat -c '%A %U:%G' /var/lib/coreshift /var/lib/coreshift/api.json` даёт `drwx--x--- root:coreshift` и `-rw-r----- root:coreshift`; `sudo -u nobody cat /var/lib/coreshift/api.json` отвечает «Permission denied».
2. Приложение из меню: виден главный экран без «Служба не запущена»; повторный запуск не открывает второе окно; ссылка `coreshift://...` из браузера открывает предложение добавить подписку.
3. Подключение:
   - `ip link show coreshift`;
   - DNS: `resolvectl status coreshift` (resolved), `cat /etc/resolv.conf` (файл, resolvconf, netconfig);
   - `curl https://www.cloudflare.com/cdn-cgi/trace` показывает IP сервера;
   - проверка утечек DNS в настройках;
   - входящие соединения из локальной сети работают (например, `ssh` на эту машину с другой).
4. Отключение: DNS как до подключения, интерфейса нет.
5. Сбой: `sudo kill -9 $(pgrep -f 'coreshiftd service run')`. Служба перезапускается, DNS восстановлен, сайты открываются. После жёсткого выключения ПК сеть работает сразу после загрузки.
6. NetworkManager без resolved: `nmcli connection up <подключение>`, через 5 с снова файл CoreShift; при отключении остаётся новый файл NetworkManager.
7. firewalld: `firewall-cmd --get-zone-of-interface=coreshift` показывает `trusted`, после отключения интерфейса в зоне нет.
8. Окно закрыто, VPN включён: VPN продолжает работать (`curl` показывает IP сервера), пока его не выключат кнопкой; «Выход» в трее выключает VPN.
9. Автозапуск: включить, выйти из сеанса и войти — CoreShift в трее (без трея — окно), VPN подключается.
10. Удаление: `apt remove`, `dnf remove`, `zypper rm`, `pacman -R` или `./uninstall.sh` останавливают службу и восстанавливают DNS. Подписки остаются; `apt purge` или `uninstall.sh --purge` удаляют и их.

Если что-то не работает, пришлите `journalctl -u coreshift -b` (OpenRC: `/var/log/coreshift.log`) и `sudo cat /var/lib/coreshift/coreshiftd.log`. URL подписок туда не пишутся. Если служба не стартует или что-то не работает из-за ограничений юнита (`CapabilityBoundingSet`, `ProtectSystem` и `ReadWritePaths`, `SystemCallFilter`, `MemoryDenyWriteExecute`, `DevicePolicy`), можно временно закомментировать их (`sudo systemctl edit --full coreshift`) и сообщить, какая строка мешала.
