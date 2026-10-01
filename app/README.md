# CoreShift: интерфейс

Flutter-приложение (пакет `coreshift`): Windows, Linux, Android и веб-демо. Общее описание проекта, сборка и выпуск — в [../README.md](../README.md).

- `lib/api/` — клиент локального API службы и демо-бэкенд (`--dart-define=DEMO=true`; веб-сборка всегда демо).
- `lib/state/` — состояние приложения и перевод ошибок движка на понятный язык.
- `lib/ui/` — страницы и виджеты.
- `lib/platform/` — трей, службы Windows, уведомления; на вебе заменяются заглушками.
- `android/` — Kotlin-часть: `VpnService`, плитка шторки, автозапуск, установка обновлений.

Версия приложения приходит из сборки (`--dart-define CORESHIFT_VERSION`, см. `lib/version.dart`); `version` в `pubspec.yaml` служит только для запуска без скриптов.
