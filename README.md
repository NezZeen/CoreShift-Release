# CoreShift

VPN-клиент для подписок (VLESS и другие протоколы) с автоматическим переключением ядер **xray → sing-box → mihomo**, если текущее перестало работать. Платформы: Windows, Linux, Android.

## Как устроено

| Папка | Что там |
| --- | --- |
| `engine/` | Движок на Go: подписки, хранилище настроек, супервизор ядер с автосвапом, слой TUN + DNS, самообновление. Работает службой на ПК (`coreshiftd`) и библиотекой внутри приложения на Android (`engine/mobile`, gomobile). |
| `app/` | Интерфейс на Flutter (Windows, Linux, Android, веб-демо). Говорит со службой по локальному HTTP API на `127.0.0.1` с токеном из `api.json`. |
| `packaging/` | Сборка и выпуск: установщик Windows (Inno Setup), APK, подписанные манифесты обновлений, публикация. Подробнее в [packaging/README.md](packaging/README.md). |
| `prototype/` | Исходный HTML-макет интерфейса. В приложении переписан на Flutter. |

Главные решения:

- Постоянный слой TUN + DNS (sing-box как TUN-фронтенд, fake-ip) отделён от сменяемого ядра. Ядра запускаются отдельными процессами с SOCKS-входом, поэтому при автосвапе TUN и DNS не пересоздаются.
- Права: на Windows служба работает от SYSTEM, а интерфейс без прав; на Linux служба systemd работает от root, а токен API читает только группа `coreshift`; на Android — `VpnService`.
- Подмена DNS всегда с журналом отката: на Windows NRPT + `strict_route`, на Linux systemd-resolved или `resolv.conf`.
- Приложение само обновляется из приватного репозитория релизов. Манифест подписан Ed25519, установщик проверяется по SHA-256.

## Быстрый старт для разработки

Нужны Go, Flutter и Git. Для Windows-сборки ещё Visual Studio (C++) и Inno Setup 6; для Android — Android SDK с NDK 28.2 и `gomobile`.

Движок:

```
cd engine
go vet ./...
go test ./...
```

Интерфейс:

```
cd app
flutter analyze
flutter test
flutter run -d windows --dart-define=DEMO=true   # с демо-бэкендом, без службы
```

Ядра (xray, sing-box, mihomo) в git не хранятся: сборка берёт их из `engine/testdata/bin`.

## Сборка и выпуск

Версия — файл `VERSION`. Номер сборки — число коммитов, пользователь его не видит.

```
powershell -ExecutionPolicy Bypass -File packaging\windows\build.ps1        # установщик Windows
powershell -ExecutionPolicy Bypass -File packaging\android\build.ps1        # APK для телефона (arm64)
powershell -ExecutionPolicy Bypass -File packaging\android\build.ps1 -Abi x86_64   # APK для эмулятора
packaging/linux/build.sh --fetch-cores                                     # .deb и .tar.gz, только на Linux
```

Сборка, установка и проверка на Linux описаны в [packaging/linux/README.md](packaging/linux/README.md).

Порядок выпуска (работа идёт в ветке `main`, релиз делается из `main`), подпись, токен и самообновление описаны в [packaging/README.md](packaging/README.md).
