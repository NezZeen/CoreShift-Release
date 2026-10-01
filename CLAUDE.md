# CoreShift: заметки для работы над кодом

Описание проекта и команды сборки — в [README.md](README.md), выпуск и самообновление — в [packaging/README.md](packaging/README.md).

## Стек и границы

- Движок — Go в `engine/`, интерфейс — Flutter в `app/`. Не предлагать Electron, Tauri или Wails: Android обязателен.
- Новый серверный код идёт в `engine/internal/...`, интерфейс — только Dart без сторонних пакетов, кроме уже подключённых (`window_manager`, `tray_manager`, `ffi`).
- Локальное API (`engine/internal/service/api.go`) защищено токеном и проверкой `Host`. Не ослаблять эти проверки и не логировать URL подписок: в них токен доступа.

## Проверки перед коммитом

```
cd engine && go vet ./... && go test ./...
cd app && flutter analyze && flutter test
```

Flutter стоит в `C:\src\flutter` (в PATH пользователя, но не обязательно в PATH сессии: добавить `C:\src\flutter\bin`). Код Dart форматируется `dart format --line-length 160`.

## Правила работы

- Ответы пользователю — на русском; в конце отчёта чек-лист «сделано / осталось».
- Файлы с кириллицей править через Write/Edit: PowerShell 5 портит кодировку при Get-Content/Set-Content.
- Работа идёт в ветке `main_test`; `main` — последний выпуск. Выпуск делается только по просьбе пользователя: перемотать `main` на `main_test`, `packaging\release.ps1 -Version X` на `main`, затем `packaging\publish.ps1`, потом перемотать `main_test`.
- Версии: патч — исправления, минорная — новые возможности, мажорная — крупные обновления. Пользователь видит только версию, не номер сборки.
- Старые релизы на GitHub и файлы в `dist/` не удалять и не переименовывать.
- Тестовые сборки публикуются как пре-релизы; у каждого выпуска есть описание изменений на русском.
- На GitHub заливаются только релизы в приватный `NezZeen/coreshift-releases`, исходники — нет.
- Токен релизов, ключ подписи обновлений и ключ подписи APK лежат в `%USERPROFILE%\.coreshift\` и в репозиторий не попадают.

## Проверка Android

Эмулятор: AVD `coreshift-test`, запуск `emulator -avd coreshift-test`. ARM-трансляция эмулятора роняет Go-движок, поэтому для него нужна сборка `build.ps1 -Abi x86_64` (ядра в `engine/testdata/bin/android-x86_64`).
