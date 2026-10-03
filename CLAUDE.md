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
- Работа идёт прямо в ветке `main`. Выпуск делается только по просьбе пользователя: `packaging\release.ps1 -Version X` на `main`, затем `packaging\publish.ps1`.
- После любых изменений кода обновлять граф: `graphify update .`.
- Версии: патч — исправления, минорная — новые возможности, мажорная — крупные обновления. Пользователь видит только версию, не номер сборки.
- Старые релизы на GitHub и файлы в `dist/` не удалять и не переименовывать.
- Тестовые сборки публикуются в закрытый репозиторий обычными релизами, чтобы их получало самообновление; у каждого выпуска есть описание изменений на русском.
- На GitHub заливаются только релизы в приватный `NezZeen/coreshift-releases`, исходники — нет.
- Токен релизов, ключ подписи обновлений и ключ подписи APK лежат в `%USERPROFILE%\.coreshift\` и в репозиторий не попадают.

## Проверка Android

Эмулятор: AVD `coreshift-test`, запуск `emulator -avd coreshift-test`. ARM-трансляция эмулятора роняет Go-движок, поэтому для него нужна сборка `build.ps1 -Abi x86_64` (ядра в `engine/testdata/bin/android-x86_64`).

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
