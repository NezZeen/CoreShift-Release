# CoreShift для Linux

Сборка и установка на ПК с Linux: Ubuntu и Debian (пакет `.deb`), Fedora, Arch и другие системы с systemd (архив `.tar.gz` со скриптом установки).

## Как устроено

| Что | Где | Права |
| --- | --- | --- |
| Приложение (Flutter) | `/opt/coreshift/coreshift`, ярлык `dev.coreshift.coreshift.desktop` | пользователь |
| Служба `coreshiftd` | `/opt/coreshift/coreshiftd`, юнит `coreshift.service` | root, с ограниченным набором capabilities |
| Ядра xray, sing-box, mihomo | `/opt/coreshift/cores` | root; служба обновляет их сама |
| Настройки, подписки, журнал DNS | `/var/lib/coreshift` | root |

- **Служба работает с загрузки системы**, а VPN включается, только пока открыто приложение: служба подключается, когда приложение открылось (если включён «Автозапуск»), и отключается через 10 секунд после того, как его закрыли. Так же, как на Windows, VPN не работает «невидимо».
- **Доступ к службе.** Приложение говорит со службой по `127.0.0.1:17900` с токеном из `/var/lib/coreshift/api.json`. Токен читают только члены группы `coreshift`:

  ```
  /var/lib/coreshift            root:coreshift 0710   члены группы открывают файлы по имени, но не видят список
  /var/lib/coreshift/api.json   root:coreshift 0640   адрес и токен
  остальное                     root 0700 / 0600      подписки, настройки, конфиги ядер, журнал DNS
  ```

  Пакет создаёт группу и добавляет в неё пользователя, который его ставил (через `sudo` или центр приложений). Других пользователей добавляют командой `sudo usermod -aG coreshift <имя>`. После добавления нужно **выйти из системы и войти снова**. Без группы приложение пишет, что делать.
- **DNS.** Если работает systemd-resolved (Ubuntu, Fedora; NetworkManager поверх него), интерфейс `coreshift` получает DNS туннеля и домен `~.`, и все запросы уходят туда. Иначе (Debian без resolved, Arch с NetworkManager, обычный `/etc/resolv.conf`) служба временно пишет свой `/etc/resolv.conf` и каждые 5 секунд возвращает его, если NetworkManager или DHCP-клиент его перезаписали. Кроме того, sing-box перехватывает любые DNS-пакеты в туннеле, так что пропущенный резолвер тоже не даёт утечки.
- **Восстановление после сбоя.** Каждое изменение DNS сначала записывается в журнал `/var/lib/coreshift/dnsguard.json`. При старте службы (с загрузки и после любого перезапуска) `ExecStartPre=coreshiftd dns recover` и сама служба откатывают то, что осталось от упавшего запуска. Журнал проверяется: служба пишет только в `/etc/resolv.conf` и восстанавливает только ссылки на файлы resolved, NetworkManager или resolvconf.
- **Программы без VPN / через VPN** работают по имени исполняемого файла (`firefox`, `telegram-desktop`, `steam`), как на Windows по `.exe`. В списке «Выбрать из запущенных» видны программы пользователей (uid от 1000), без служб.
- **Обновления CoreShift** на Linux ставит пакетный менеджер, а не служба: в настройках написано «Обновляйте через пакет». Почему так, ниже. Ядра обновляются сами, как на Windows.
- **Трей** — StatusNotifierItem. В KDE, Xfce, Cinnamon, MATE и Ubuntu (расширение AppIndicator) он есть сразу. В «чистом» GNOME (Fedora, Debian) нужно расширение [AppIndicator](https://extensions.gnome.org/extension/615/appindicator-support/), иначе значка нет. Окно тогда открывается повторным запуском из меню.
- **Ссылки `coreshift://`** открывают CoreShift через ярлык (`MimeType=x-scheme-handler/coreshift`). Если CoreShift уже запущен, второй запуск передаёт ссылку ему и закрывается.
- **Автозапуск** — файл `~/.config/autostart/dev.coreshift.coreshift.desktop`, его пишет приложение по переключателю «Автозапуск».

### Почему нет самообновления

Самообновление выключено намеренно:

1. Файлы в `/opt/coreshift` принадлежат пакету. Если служба подменит их сама, `dpkg` будет считать, что стоит старая версия, и следующее обновление или удаление пакета может сломаться.
2. Служба работает от root. Ставить пакеты от root по команде из интернета — это ещё одна поверхность атаки. Подпись снижает риск, но не убирает его. Пакетный менеджер делает то же самое проверенным путём.
3. Пакеты бывают разные (`.deb`, архив, в будущем `.rpm`), и для каждого нужен свой установщик. Пакетный менеджер уже умеет ставить каждый из них.

Поэтому на Linux служба отвечает «off» с причиной `linux: CoreShift is updated with its package`, а приложение пишет «Обновляйте через пакет». Манифест Linux отдельный (`latest-linux.json`, только `.deb`), чтобы Linux-служба никогда не взяла установщик Windows.

## Сборка

Собирать нужно на Linux того же процессора: Flutter не собирает Linux-приложения кросс-компиляцией. Для arm64 нужна arm64-машина, например раннер GitHub `ubuntu-24.04-arm`.

1. Поставить инструменты (Ubuntu 24.04 / Debian 12):

   ```
   sudo apt install git curl unzip xz-utils clang cmake ninja-build pkg-config \
       libgtk-3-dev liblzma-dev libstdc++-12-dev libx11-dev libxi-dev dpkg-dev
   ```

   Go 1.26+ взять с https://go.dev/dl/, Flutter — по инструкции https://docs.flutter.dev/get-started/install/linux (нужна та же версия, что на Windows: `flutter --version` на ПК). Проверка: `flutter doctor` без ошибок в разделе Linux toolchain.

2. Скачать ядра. Команда берёт последние стабильные версии с GitHub, сверяет SHA-256 и проверяет, что каждое ядро запускается:

   ```
   cd engine && go run ./cmd/coreshiftd cores fetch -dir testdata/bin/linux-amd64 && cd ..
   ```

   Или положить в `engine/testdata/bin/linux-amd64` файлы `xray`, `sing-box`, `mihomo` из релизов (`Xray-linux-64.zip`, `sing-box-<версия>-linux-amd64.tar.gz`, `mihomo-linux-amd64-v1-<версия>.gz`). Ядра называются именно так, без версий.

3. Проверить код:

   ```
   cd engine && go vet ./... && go test ./... && cd ..
   cd app && flutter analyze && flutter test && cd ..
   ```

   На Linux при этом выполняются и тесты, которые на Windows пропускаются: `secure_linux_test.go` (права на `api.json`), `TestLinuxResolvConfSymlinkRestored`, `TestLinuxApplyRefusesUntrustedSymlink`, `TestRunningIncludesThisTest`.

4. Собрать:

   ```
   packaging/linux/build.sh            # или с --fetch-cores вместо шага 2
   ```

   Результат: `dist/coreshift_<версия>_amd64.deb` и `dist/coreshift-<версия>-linux-amd64.tar.gz`. Между релизами в имени есть номер сборки (`0.6.5+b130`, `0.6.5-b130`), а `-dirty` означает незакоммиченные изменения.

## Установка

Ubuntu, Debian, Mint:

```
sudo apt install ./coreshift_0.6.5_amd64.deb
```

Fedora, Arch и другие:

```
tar xzf coreshift-0.6.5-linux-amd64.tar.gz
cd coreshift-0.6.5-linux-amd64
sudo ./install.sh
```

Затем **выйти из системы и войти снова**: это нужно, чтобы вступило в силу членство в группе `coreshift`.

Удаление: `sudo apt remove coreshift` (`purge` удалит и подписки) или `sudo ./uninstall.sh [--purge]`.

## План проверки

Лучше всего начать с Ubuntu 24.04 (systemd-resolved, NetworkManager, GNOME с AppIndicator). Затем, если будет время, проверить Debian 12 без resolved, Fedora 40+ и Arch. Подойдёт и виртуальная машина (VirtualBox или GNOME Boxes), если её сеть идёт через NAT.

### 1. Установка и служба

```
sudo apt install ./coreshift_*.deb
systemctl status coreshift          # active (running)
id                                  # после повторного входа: есть группа coreshift
sudo ls -l /var/lib/coreshift       # api.json: -rw-r----- root coreshift
stat -c '%A %U:%G' /var/lib/coreshift   # drwx--x--- root:coreshift
cat /var/lib/coreshift/api.json     # читается от своего пользователя
sudo -u nobody cat /var/lib/coreshift/api.json   # Permission denied
journalctl -u coreshift -n 50       # «API listening on 127.0.0.1:17900»
```

### 2. Приложение

- Открыть CoreShift из меню. Окно открывается, в нём свой заголовок, сверху нет второго, системного.
- Значок в трее: левый клик открывает окно, в правом меню есть «Подключить», «Сервер», «Выход».
- Закрыть окно крестиком: окно прячется, приходит уведомление «CoreShift работает в трее».
- Ещё раз запустить CoreShift из меню: открывается то же окно, вторая копия не появляется (`pgrep -c coreshift` = 1).
- Настройки → «Обновления»: текст «Обновляйте через пакет…», кнопки «Проверить сейчас» нет.
- Настройки → «Защита от утечек DNS»: без упоминания Windows.

### 3. VPN и DNS (Ubuntu, systemd-resolved)

Добавить подписку и подключиться, затем:

```
ip link show coreshift              # интерфейс есть
resolvectl status coreshift         # DNS Servers: 172.19.0.2, DNS Domain: ~.
resolvectl query example.com        # ответ через coreshift
curl https://ifconfig.me            # IP VPN-сервера
```

В приложении: Настройки → проверка утечек DNS → «Утечки нет». Затем отключиться и проверить, что `resolvectl status coreshift` пишет, что такого интерфейса нет, а сайты открываются.

### 4. DNS без resolved (Debian 12 или Arch с NetworkManager)

```
ls -l /etc/resolv.conf              # до подключения
# подключиться
cat /etc/resolv.conf                # «Generated by CoreShift…», nameserver 172.19.0.2
sudo nmcli connection up <ваше подключение>   # NetworkManager перепишет файл
sleep 6; cat /etc/resolv.conf       # через ≤5 с снова файл CoreShift
# отключиться
cat /etc/resolv.conf                # содержимое NetworkManager, не старое
```

### 5. Сбой и восстановление

```
# подключиться, затем убить службу так, будто она упала:
sudo systemctl kill -s KILL coreshift
sleep 5; systemctl status coreshift      # systemd перезапустил её
resolvectl status coreshift              # нет интерфейса, DNS системы в порядке
cat /etc/resolv.conf                     # (без resolved) исходный файл
```

Ещё один вариант — подключиться и выключить машину кнопкой питания (для ВМ — «Power off»). После загрузки сайты должны открываться сразу, ещё до запуска CoreShift.

### 6. Ссылки, автозапуск, отключение без приложения

- Открыть в браузере ссылку `coreshift://...` (кнопка «Добавить в приложение» в панели провайдера, или `xdg-open '<та же ссылка coreshift://…>'` в терминале). CoreShift предлагает добавить подписку, и в работающей копии тоже.
- Включить «Автозапуск», затем проверить, что файл `~/.config/autostart/dev.coreshift.coreshift.desktop` появился. Выйти из системы и войти: CoreShift в трее, VPN подключается. Выключить «Автозапуск» — файл исчезает.
- Подключиться, затем в трее нажать «Выход». VPN отключается сразу. А при `pkill -KILL coreshift` VPN отключается через 10 с: смотреть `journalctl -u coreshift -f`, там «the app is closed; disconnecting».

### 7. Программы и ядра

- Правила → «Программы без VPN» → «Выбрать из запущенных»: в списке firefox и другие программы пользователя, системных служб нет. Добавить `firefox`, переподключиться, и ifconfig.me в Firefox показывает домашний IP, а `curl` — IP VPN.
- Настройки → Ядра: версии видны, обновление ядра проходит (файлы в `/opt/coreshift/cores` заменяются).
- Остановить xray (`sudo pkill -f cores/xray`): автосвап на sing-box, VPN работает.

### 8. Удаление

```
sudo apt remove coreshift
resolvectl status                    # следов coreshift нет
ls /var/lib/coreshift                # подписки остались
sudo apt purge coreshift             # теперь и /var/lib/coreshift удалён
```

Если что-то не работает, пришлите `journalctl -u coreshift -b` и `/var/lib/coreshift/coreshiftd.log` (`sudo cat`). URL подписок там не пишутся.

Если служба не стартует из-за ограничений в юните (`CapabilityBoundingSet`, `ProtectSystem`), можно временно закомментировать эти строки (`sudo systemctl edit --full coreshift`) и сообщить, какая из них мешала.
