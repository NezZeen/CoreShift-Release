<div align="center">

# CoreShift

**VPN-клиент для Windows и Android, который сам держит соединение.**
Вставили подписку, нажали одну кнопку, дальше CoreShift всё делает сам.

[![Скачать для Windows](https://img.shields.io/badge/Windows-скачать-0078D4?style=for-the-badge&logo=windows&logoColor=white)](https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift-Setup.exe)
[![Скачать для Android](https://img.shields.io/badge/Android-скачать%20APK-3DDC84?style=for-the-badge&logo=android&logoColor=white)](https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift.apk)

[![Последняя версия](https://img.shields.io/github/v/release/NezZeen/CoreShift-Release?label=версия&color=6D8CFF)](https://github.com/NezZeen/CoreShift-Release/releases/latest)
[![Загрузки](https://img.shields.io/github/downloads/NezZeen/CoreShift-Release/total?label=загрузки&color=34D399)](https://github.com/NezZeen/CoreShift-Release/releases)

<img src="assets/desktop-home.png" alt="CoreShift на Windows" width="760">

</div>

## Почему CoreShift

**🔁 Три ядра вместо одного.** Внутри сразу Xray, sing-box и mihomo. Если ядро упало, не принимает конфиг или перестало пропускать трафик, CoreShift сам переключается на следующее, и интернет не пропадает. Когда основное ядро снова в порядке, CoreShift может вернуться на него сам.

**🛡 Обход блокировок (DPI).** Одним переключателем CoreShift режет начало защищённого соединения на части, и фильтры провайдера перестают его узнавать.

**⚡ Сам находит быстрый сервер.** CoreShift замеряет пинг до всех серверов и одной кнопкой подключается к самому быстрому. Пинг можно мерить до сервера или настоящим запросом через ядро: так видно, работает ли сервер на самом деле.

**🧩 Подписка добавляется как удобно:**
- кнопкой «Добавить в приложение» на странице подписки панели. CoreShift понимает ссылки для себя (`coreshift://`), а также для Happ, v2rayNG, v2RayTun, Hiddify, Clash, sing-box и других;
- из буфера обмена: скопировали ссылку, открыли CoreShift, он сам предложит её добавить;
- по QR-коду: показали подписку на ПК, отсканировали телефоном.

Перед добавлением CoreShift спрашивает подтверждение и не показывает ключ из ссылки.

**🗂 Все форматы подписок.** Поддерживаются ссылки `https://…`, списки серверов VLESS (Reality, XHTTP, gRPC…), VMess, Trojan, Shadowsocks, Hysteria2, TUIC, AnyTLS, WireGuard, а также JSON Xray (например, от Remnawave) и конфиги Clash.

**🚦 Гибкие правила.** Можно выбрать, что идёт через VPN:
- всё, кроме исключений, или только выбранное;
- российские сайты напрямую одним переключателем;
- готовые наборы популярных сервисов в один клик;
- свои сайты: через VPN, мимо VPN или заблокировать совсем;
- отдельные программы на Windows (игры, торренты, банки) и приложения на Android — через VPN или мимо него.

**🔒 Без утечек.** CoreShift перехватывает DNS и может блокировать DNS-over-TLS. Встроенная проверка показывает, видят ли сайты ваш настоящий DNS.

**⏰ Напомнит продлить подписку.** За несколько дней до конца срока и при 90% трафика на главной появится предупреждение с кнопкой «Продлить» и придёт уведомление.

**📊 Всё видно.** На главной ваш IP-адрес и страна, трафик за неделю, тест скорости (загрузка, отдача и задержка) через VPN или без него. Кнопка «Поддержка» ведёт в чат провайдера, если он его указал.

**🔄 Обновляется сам.** Обновления подписаны и проверяются перед установкой. На ПК обновление ставится, когда VPN выключен, и не рвёт соединение.

**🌗 Тёмная и светлая темы**, автоподключение при запуске, свёртывание в трей.

<div align="center">
<img src="assets/phone-home.png" alt="Главная на телефоне" width="260">
&nbsp;&nbsp;
<img src="assets/phone-servers.png" alt="Серверы на телефоне" width="260">
</div>

## Установка

| Система | Скачать |
|---|---|
| Windows 10 и 11 (64-bit) | [CoreShift-Setup.exe](https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift-Setup.exe) |
| Android 7 и новее (64-bit ARM) | [CoreShift.apk](https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift.apk) |

1. Скачайте файл для своей системы и установите его. На Android система попросит разрешить установку из браузера или файлового менеджера.
2. Добавьте подписку: кнопкой на странице подписки, из буфера обмена, по QR-коду или вставив ссылку вручную.
3. Нажмите кнопку подключения.

Все версии и что в них нового — на странице [Releases](https://github.com/NezZeen/CoreShift-Release/releases).

## Для владельцев панелей

Чтобы на странице подписки (Remnawave и других) появилась кнопка «Добавить в CoreShift», используйте схему:

```
coreshift://add/<ссылка на подписку>
```

Для Remnawave в конфиге страницы подписки:
- `"urlScheme": "coreshift://add/"`;
- ссылка на скачивание для Windows: `https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift-Setup.exe`;
- ссылка на скачивание для Android: `https://github.com/NezZeen/CoreShift-Release/releases/latest/download/CoreShift.apk`.

Обе ссылки на скачивание всегда ведут на последнюю версию.
