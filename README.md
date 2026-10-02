# CoreShift

VPN-клиент для Windows и Android. Добавьте ссылку на подписку от провайдера, и CoreShift сам замерит пинг, подскажет самый быстрый сервер и подключит все приложения через VPN.

## Скачать

| Система | Файл |
|---|---|
| Windows 10 и 11 (64-bit) | [CoreShift-Setup.exe](https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift-Setup.exe) |
| Android 7 и новее (64-bit ARM) | [CoreShift.apk](https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift.apk) |

Эти ссылки всегда ведут на последнюю версию. Все версии — на странице [Releases](https://github.com/NezZeen/CoreShift-Release/releases).

На Android система попросит разрешить установку приложений из браузера или файлового менеджера.

## Что умеет

- Подписки: ссылки `https://…`, списки серверов `vless://`, `vmess://`, `trojan://`, `ss://`, `hy2://`, `tuic://` и другие, JSON Xray и Clash.
- Добавление подписки одной кнопкой со страницы подписки панели (`coreshift://`), из буфера обмена или по QR-коду.
- Три ядра: Xray, sing-box и mihomo. Если одно перестаёт работать, CoreShift переключается на другое.
- Обход блокировок (DPI), раздельное туннелирование по сайтам и приложениям, защита от утечек DNS.
- Предупреждения об окончании подписки и трафика, тест скорости.
- Обновляется сам.

## Добавить кнопку на страницу подписки (Remnawave и другие)

Ссылка добавления подписки:

```
coreshift://add/<ссылка на подписку>
```
