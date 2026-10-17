/// The UI's texts of the settings, the cores, proxy mode and the app pickers, keys "settings.…" (see strings.dart):
/// Russian, the source of truth, and English, key for key.
library;

const settingsRu = <String, String>{
  // Settings page: the page and the self-update line.
  'settings.title': 'Настройки',
  'settings.update.off.android': 'Эта сборка не обновляется сама: так бывает у сборки для разработки.',
  'settings.update.off.linux': 'Эта сборка не ищет новые версии: так бывает у сборки для разработки. Обновляйте через пакет.',
  'settings.update.available': 'Доступна новая версия {label}. Скачайте пакет для своей системы и установите поверх этой.',
  'settings.update.off': 'Эта копия не обновляется сама: так бывает у сборки для разработки или у службы старше 0.3.0.',
  'settings.update.checking': 'Проверяю…',
  'settings.update.downloading': 'Скачиваю версию {label}…',
  'settings.update.ready.waiting': 'Скачана версия {label}. Установится сама после отключения VPN.',
  'settings.update.ready.android': 'Скачана версия {label}. Android попросит подтвердить установку.',
  'settings.update.ready': 'Скачана версия {label}.',
  'settings.update.installing': 'Устанавливаю {label}. CoreShift перезапустится сам.',
  'settings.update.latest.manual': 'Установлена последняя версия. Проверено {when}. Новые версии ставятся пакетом.',
  'settings.update.never': 'Проверка ещё не проводилась.',
  'settings.update.latest': 'Установлена последняя версия. Проверено {when}.',

  // Settings page: «Приложение».
  'settings.app.title': 'Приложение',
  'settings.autostart.title': 'Автозапуск',
  'settings.autostart.desc.android': 'Подключать выбранный сервер при включении телефона и при открытии CoreShift',
  'settings.autostart.desc.linux': 'Запускать CoreShift в трее при входе в систему и сразу подключать выбранный сервер',
  'settings.autostart.desc.windows': 'Запускать CoreShift в трее при входе в Windows и сразу подключать выбранный сервер',
  'settings.battery.title': 'Экономия батареи',
  'settings.battery.desc': 'Когда экран выключен или включено энергосбережение, связь с сервером проверяется реже и телефон дольше спит',
  'settings.notifications.title': 'Уведомления',
  'settings.notifications.desc': 'Сообщать о смене ядра, обрывах связи и окончании подписки, когда окно CoreShift свёрнуто или в трее',
  'settings.clipboard.title': 'Ссылки из буфера обмена',
  'settings.clipboard.desc': 'Предлагать добавить подписку, когда скопирована ссылка на неё',
  'settings.theme.title': 'Тема',
  'settings.theme.dark': 'Тёмная',
  'settings.theme.light': 'Светлая',
  'settings.theme.system': 'Как в системе',

  // Settings page: «Подключение».
  'settings.network.title': 'Подключение',
  'settings.tun.title': 'Все приложения через VPN',
  'settings.tun.desc': 'Режим TUN. Если выключить, через VPN пойдут только программы с прокси SOCKS5 или HTTP 127.0.0.1:{port}',
  'settings.ipv6.title': 'IPv6 через туннель',
  'settings.ipv6.desc':
      'IPv6-трафик тоже идёт через VPN. Если выключить, IPv6 при подключении не работает вовсе и сайты открываются по IPv4. '
      'Выключите, если какие-то сайты перестали открываться',
  'settings.dpi.title': 'Обход блокировок (DPI)',
  'settings.dpi.desc':
      'Делит начало защищённого соединения с сервером на части, чтобы провайдер не узнал его. '
      'Включите, если VPN то работает, то нет. Работает в Xray и sing-box, mihomo подключается без этого',
  'settings.switch_server.title': 'Менять сервер, если он не отвечает',
  'settings.switch_server.desc':
      'Если сервер не отвечает, а интернет работает, CoreShift подключит следующий сервер той же подписки, который отвечает. '
      'Автовыбор от панели провайдера меняет сервер всегда',
  'settings.latency.title': 'Проверка задержки',
  'settings.latency.desc':
      'Через сервер — запрос через сам сервер: видно, что он действительно пропускает трафик.\n'
      'TCP-пинг — только проверка, что порт сервера открыт; быстрее и экономнее для батареи и трафика',
  'settings.latency.proxy': 'Через сервер',
  'settings.latency.ping': 'TCP-пинг',
  'settings.latency.proxy.long': 'Через сервер (точнее)',
  'settings.latency.ping.long': 'TCP-пинг (быстрее)',
  'settings.speed.title': 'Проверка скорости',
  'settings.speed.desc':
      'Сколько данных скачивает один сервер при проверке скорости на странице «Серверы»: проверка идёт до 5 секунд или до этого объёма. '
      'Больше — точнее, но и трафика уходит больше',
  'settings.speed.mb': '{n} МБ',

  // Settings page: «Дополнительно».
  'settings.more.title': 'Дополнительно',
  'settings.more.sub': 'обновление подписок, User-Agent, DNS-серверы, защита DNS по отдельности',
  'settings.subs.label': 'Подписки',
  'settings.subs.auto.title': 'Обновлять подписки сами',
  'settings.subs.auto.desc': 'С интервалом, который задаёт панель провайдера',
  'settings.ua.title': 'User-Agent',
  'settings.ua.desc': 'Меняйте, только если провайдер просит указать конкретное приложение',
  'settings.dns.remote.title': 'DNS через VPN',
  'settings.dns.remote.desc': 'DoH, DoT, DoQ или обычный адрес: https://1.1.1.1/dns-query, tls://dns.google, 8.8.8.8',
  'settings.dns.direct.title': 'DNS напрямую',
  'settings.dns.direct.desc': 'Для сайтов без VPN. Пусто — DNS, который был в системе до подключения',
  'settings.dns.direct.hint': 'системный',
  'settings.fakeip.title': 'Fake-IP',
  'settings.fakeip.desc': 'Имена резолвятся на стороне сервера — быстрее и без утечек DNS',

  // Settings page: the guards against DNS leaks.
  'settings.guard.doh.title': 'DNS браузеров только через VPN',
  'settings.guard.doh.desc': 'Firefox не включает свой DNS-over-HTTPS сам, а если он включён вручную, его запросы идут через туннель',
  'settings.guard.dot.title': 'Блокировать DNS-over-TLS (порт 853)',
  'settings.guard.dot.desc.android':
      'Android в режиме «Частный DNS: автоматически» перейдёт на обычный DNS. Если «Частный DNS» задан вручную, сайты перестанут открываться',
  'settings.guard.dot.desc': 'Программы с собственным DNS-over-TLS перейдут на DNS через туннель',
  'settings.guard.strict.title': 'Строгий DNS в Windows',
  'settings.guard.strict.desc': 'Запретить Windows опрашивать DNS других сетевых адаптеров в обход туннеля',
  'settings.guard.title': 'Защита от утечек DNS',
  'settings.guard.desc.android':
      'DNS браузеров и DNS-over-TLS только через VPN. Если в Android «Частный DNS» задан вручную, с защитой сайты перестанут открываться',
  'settings.guard.desc.windows': 'DNS браузеров, DNS-over-TLS и DNS других сетевых адаптеров Windows не уходят мимо туннеля',
  'settings.guard.desc': 'DNS браузеров и DNS-over-TLS не уходят мимо туннеля',
  'settings.guard.partly': 'Включено не всё: {on} из {total}. Включите, чтобы защитить всё',

  // Settings page: the app itself.
  'settings.version.title': 'Версия',
  'settings.version.commit': 'коммит {commit}',
  'settings.service_version.title': 'Версия службы',
  'settings.updates.title': 'Обновления',
  'settings.updates.install.android': 'Обновить',
  'settings.updates.install': 'Установить сейчас',
  'settings.updates.install.tip': 'VPN отключится на время установки и подключится снова',
  'settings.updates.download': 'Скачать',
  'settings.updates.check': 'Проверить сейчас',
  'settings.updates.auto.title': 'Устанавливать обновления автоматически',
  'settings.updates.auto.desc':
      'Раз в день CoreShift проверяет новую версию и ставит её сам, когда VPN выключен. '
      'Если VPN включён, установка подождёт.',
  'settings.disclaimer.title': 'Отказ от ответственности',
  'settings.disclaimer.desc': 'Некоммерческая программа «как есть»: серверы и подписки предоставляет ваш провайдер',
  'settings.disclaimer.read': 'Прочитать',

  // Settings page: the cores card.
  'settings.cores.title': 'Ядра',
  'settings.cores.single': 'Работает одно ядро, без переключения при сбое',
  'settings.cores.chain': 'Если ядро перестанет работать, CoreShift переключится на следующее',

  // Cores page.
  'cores.title': 'Ядра',
  'cores.subtitle': 'Установленные ядра и порядок, в котором CoreShift их пробует.',
  'cores.update.installing': 'Устанавливается {latest}…',
  'cores.update.soon': 'Версия {latest} скоро установится сама',
  'cores.pill.running': 'работает',
  'cores.pill.failed': 'сбой',
  'cores.item.absent.android': 'нет в этой сборке',
  'cores.item.absent': 'нет в папке ядер',
  'cores.item.installed': 'установлено',
  'cores.item.version': 'версия {version}',
  'cores.switch.last': 'Нужно хотя бы одно ядро',
  'cores.switch.off': 'Не использовать это ядро',
  'cores.switch.on': 'Использовать',
  'cores.updates.android': 'Ядра обновляются вместе с приложением.',
  'cores.updates': 'CoreShift обновляет ядра сам: ищет новые версии раз в день и ставит их, когда VPN выключен.',
  'cores.list.sub.single': 'работает только одно',
  'cores.list.sub': 'сверху — предпочтительное',
  'cores.list.note.single': 'Включено одно ядро: {name}. Если оно перестанет работать, переключения не будет.',
  'cores.list.note':
      'При подключении берётся первое ядро из списка, которое поддерживает протокол сервера. '
      'Несовместимые пропускаются, остальные становятся резервом.',
  'cores.backup.title': 'Работает резервное ядро: {name}',
  'cores.backup.text': '{primary} перестало работать{why}. Вернуть его можно сразу: сначала оно проверяется в фоне.',
  'cores.backup.return': 'Вернуть {primary}',
  'cores.rules.title': 'Переключение при сбое',
  'cores.rules.return.title': 'Возвращаться к основному ядру',
  'cores.rules.return.before': 'пробовать основное ядро через',
  'cores.rules.return.after': 'мин работы на резерве',
  'cores.rules.return.stay': 'Оставаться на резервном ядре до переподключения',
  'cores.rules.more': 'Дополнительно',
  'cores.rules.more.sub': 'когда ядро считается неработающим',
  'cores.rules.health.title': 'Проверка связи',
  'cores.rules.health.before': 'сбой после',
  'cores.rules.health.middle': 'неудач подряд, интервал',
  'cores.rules.health.after': 'с',
  'cores.rules.latency.title': 'Высокая задержка',
  'cores.rules.latency.before': 'считать сбоем ответ дольше',
  'cores.rules.latency.after': 'мс',
  'cores.rules.latency.off': 'Медленные ответы не считаются сбоем',
  'cores.rules.url.title': 'Адрес проверки связи',
  'cores.rules.url.desc': 'Отвечает 204 или 200. Запасные: Google, Apple',
  'cores.rules.note': 'Сбой процесса ядра, отвергнутый конфиг и неподдерживаемый протокол переключают ядро всегда.',

  // Proxy mode (without TUN).
  'proxy.system.title': 'Системный прокси',
  'proxy.system.desc':
      'Если «Все приложения» не включаются: CoreShift сам пропишет прокси в системе. '
      'Работает для браузеров и большинства программ, но не для игр и всего, что прокси не понимает',
  'proxy.only.title': 'Прокси без VPN',
  'proxy.only.desc':
      'VPN не включается: через CoreShift идут только приложения, настроенные на прокси. '
      'Для случаев, когда место VPN занято другим VPN или рабочим профилем, или нужен только Telegram или браузер',
  'proxy.copied': '{what} скопирован',
  'proxy.telegram.failed': 'Telegram не открылся: установлен ли он?',
  'proxy.copy': 'Скопировать',
  'proxy.access.title': 'Прокси для приложений',
  'proxy.access.address': 'Адрес',
  'proxy.access.port': 'Порт',
  'proxy.access.login': 'Логин',
  'proxy.access.password': 'Пароль',
  'proxy.access.no_credentials': 'Логина и пароля ещё нет: они появятся после перезапуска CoreShift',
  'proxy.access.hide': 'Скрыть',
  'proxy.access.show': 'Показать',
  'proxy.access.telegram': 'Открыть в Telegram',
  'proxy.access.new_password': 'Новый пароль',
  'proxy.access.howto':
      'В браузере или приложении с настройкой прокси выберите SOCKS5 (или HTTP), адрес 127.0.0.1, порт {port}, этот логин и пароль. '
      'Остальные приложения идут мимо CoreShift.',
  'proxy.access.wifi': 'Прокси в настройках Wi-Fi Android принимает только HTTP без пароля: для него включите «HTTP без пароля».',
  'proxy.open_http.title': 'HTTP без пароля',
  'proxy.open_http.desc': 'Любое приложение на телефоне сможет пользоваться прокси. Нужно только для прокси в настройках Wi-Fi',
  'proxy.open_http.on': 'Включено: любое приложение на телефоне может пользоваться прокси',

  // The journal and the session log: the mode without TUN, the system proxy.
  'proxy.mode.android': 'прокси без VPN',
  'proxy.mode.system': 'системный прокси',
  'proxy.mode.only': 'только прокси',
  'proxy.report.applied': 'системный прокси включён ({names}): браузеры и программы идут через {address}; прежние настройки сохранены',
  'proxy.report.unsupported':
      'системный прокси не настроен: рабочий стол не поддерживается (нужен GNOME или KDE). '
      'CoreShift работает как обычный прокси: укажите в программах SOCKS5 или HTTP {address}',
  'proxy.report.restored': 'системный прокси выключен, прежние настройки восстановлены ({names})',
  'proxy.report.changed': 'системный прокси уже изменён вручную ({names}): оставлен как есть',
  'proxy.report.error': 'системный прокси: {error}',
  'proxy.report.pending': 'прежние настройки прокси вернутся при следующем входе в систему или запуске CoreShift',
  'proxy.err.no_daemon': 'не найден coreshiftd',
  'proxy.err.no_answer': 'coreshiftd ничего не ответил',
  'proxy.err.web': 'недоступно в браузере',

  // Program lists (desktop).
  'apps.direct.title': 'Программы без VPN',
  'apps.direct.sub': 'игры, торренты, банки',
  'apps.direct.desc':
      'Эти программы работают напрямую, как без VPN, где бы они ни были установлены. Удобно для игр (меньше пинг), '
      'торрентов и банковских приложений, которые не пускают из-за границы.',
  'apps.direct.empty': 'Пока пусто — все программы идут через VPN',
  'apps.direct.badge': 'без VPN',
  'apps.direct.remove': 'Снова через VPN',
  'apps.proxy.title': 'Программы через VPN',
  'apps.proxy.sub': 'весь их трафик',
  'apps.proxy.empty': 'Пока пусто',
  'apps.proxy.badge': 'через VPN',
  'apps.proxy.remove': 'Убрать из списка',
  'apps.add': 'Добавить',
  'apps.pick': 'Выбрать из запущенных',
  'apps.picker.title': 'Запущенные программы',
  'apps.picker.hint.linux': 'Запустите нужную программу, если её нет в списке. Системные программы не показываются.',
  'apps.picker.hint.windows': 'Запустите нужную программу, если её нет в списке. Системные программы Windows не показываются.',
  'apps.picker.search': 'Поиск',
  'apps.picker.empty': 'Ничего не нашлось',
  'apps.picker.refresh': 'Обновить список',
  'apps.done': 'Готово',

  // Android: the apps that use the VPN.
  'apps.android.title': 'Приложения',
  'apps.android.all': 'Все приложения',
  'apps.android.all.desc': 'Все приложения идут через VPN по правилам ниже',
  'apps.android.exclude': 'Кроме выбранных',
  'apps.android.exclude.desc': 'Выбранные работают напрямую, как без VPN',
  'apps.android.only': 'Только выбранные',
  'apps.android.only.desc': 'Через VPN идут только они, остальные напрямую',
  'apps.android.none.only': 'Ничего не выбрано: пока через VPN идут все',
  'apps.android.none': 'Ничего не выбрано',
  'apps.android.chosen': 'Выбрано: {n}',
  'apps.android.select': 'Выбрать',
  'apps.android.picker.only': 'Через VPN',
  'apps.android.picker.exclude': 'Без VPN',
  'apps.android.picker.search': 'Поиск приложения',
  'apps.android.picker.system': 'Показывать системные',
  'apps.android.picker.empty': 'Ничего не найдено',
};

const settingsEn = <String, String>{
  // Settings page: the page and the self-update line.
  'settings.title': 'Settings',
  'settings.update.off.android': 'This build does not update itself: that is normal for a development build.',
  'settings.update.off.linux': 'This build does not look for new versions: that is normal for a development build. Update through the package.',
  'settings.update.available': 'A new version {label} is available. Download the package for your system and install it over this one.',
  'settings.update.off': 'This copy does not update itself: that is normal for a development build or a service older than 0.3.0.',
  'settings.update.checking': 'Checking…',
  'settings.update.downloading': 'Downloading version {label}…',
  'settings.update.ready.waiting': 'Version {label} is downloaded. It installs itself once the VPN is off.',
  'settings.update.ready.android': 'Version {label} is downloaded. Android will ask you to confirm the install.',
  'settings.update.ready': 'Version {label} is downloaded.',
  'settings.update.installing': 'Installing {label}. CoreShift will restart itself.',
  'settings.update.latest.manual': 'You have the latest version. Checked {when}. New versions come with the package.',
  'settings.update.never': 'No check has been made yet.',
  'settings.update.latest': 'You have the latest version. Checked {when}.',

  // Settings page: «Приложение».
  'settings.app.title': 'App',
  'settings.autostart.title': 'Launch at startup',
  'settings.autostart.desc.android': 'Connect the selected server when the phone turns on and when CoreShift opens',
  'settings.autostart.desc.linux': 'Start CoreShift in the tray at login and connect the selected server right away',
  'settings.autostart.desc.windows': 'Start CoreShift in the tray when you sign in to Windows and connect the selected server right away',
  'settings.battery.title': 'Battery saving',
  'settings.battery.desc': 'With the screen off or power saving on, the server is checked less often and the phone sleeps longer',
  'settings.notifications.title': 'Notifications',
  'settings.notifications.desc':
      'Tell about a core switch, dropped connections and the end of a subscription while the CoreShift window is minimized or in the tray',
  'settings.clipboard.title': 'Links from the clipboard',
  'settings.clipboard.desc': 'Offer to add a subscription when its link is copied',
  'settings.theme.title': 'Theme',
  'settings.theme.dark': 'Dark',
  'settings.theme.light': 'Light',
  'settings.theme.system': 'As in system',

  // Settings page: «Подключение».
  'settings.network.title': 'Connection',
  'settings.tun.title': 'All apps through VPN',
  'settings.tun.desc': 'TUN mode. If turned off, only programs using the SOCKS5 or HTTP proxy 127.0.0.1:{port} go through the VPN',
  'settings.ipv6.title': 'IPv6 through the tunnel',
  'settings.ipv6.desc':
      'IPv6 traffic goes through the VPN too. If turned off, IPv6 does not work at all while connected and sites open over IPv4. '
      'Turn it off if some sites stopped opening',
  'settings.dpi.title': 'Anti-censorship (DPI)',
  'settings.dpi.desc':
      'Splits the start of the secure connection to the server into pieces so the provider cannot recognise it. '
      'Turn it on if the VPN works on and off. Works in Xray and sing-box; mihomo connects without it',
  'settings.switch_server.title': 'Switch server if it does not respond',
  'settings.switch_server.desc':
      'If the server does not respond but the internet works, CoreShift connects the next responding server of the same subscription. '
      'Auto-select from the provider panel always switches the server',
  'settings.latency.title': 'Latency check',
  'settings.latency.desc':
      'Through server: a request through the server itself, so you see it really passes traffic.\n'
      'TCP ping: only checks that the server port is open; faster and lighter on battery and data',
  'settings.latency.proxy': 'Through server',
  'settings.latency.ping': 'TCP ping',
  'settings.latency.proxy.long': 'Through server (exact)',
  'settings.latency.ping.long': 'TCP ping (faster)',
  'settings.speed.title': 'Speed check',
  'settings.speed.desc':
      'How much data one server downloads during a speed test on the “Servers” page: the test runs up to 5 seconds or until this amount. '
      'More is more exact, but uses more data',
  'settings.speed.mb': '{n} MB',

  // Settings page: «Дополнительно».
  'settings.more.title': 'Advanced',
  'settings.more.sub': 'subscription updates, User-Agent, DNS servers, DNS protection one by one',
  'settings.subs.label': 'Subscriptions',
  'settings.subs.auto.title': 'Update subscriptions automatically',
  'settings.subs.auto.desc': 'At the interval set by the provider panel',
  'settings.ua.title': 'User-Agent',
  'settings.ua.desc': 'Change only if the provider asks you to name a specific app',
  'settings.dns.remote.title': 'DNS through VPN',
  'settings.dns.remote.desc': 'DoH, DoT, DoQ or a plain address: https://1.1.1.1/dns-query, tls://dns.google, 8.8.8.8',
  'settings.dns.direct.title': 'Direct DNS',
  'settings.dns.direct.desc': 'For sites outside the VPN. Empty: the DNS the system had before connecting',
  'settings.dns.direct.hint': 'system',
  'settings.fakeip.title': 'Fake-IP',
  'settings.fakeip.desc': 'Names are resolved on the server side: faster and no DNS leaks',

  // Settings page: the guards against DNS leaks.
  'settings.guard.doh.title': 'Browser DNS only through VPN',
  'settings.guard.doh.desc': 'Firefox does not turn on its own DNS-over-HTTPS by itself, and if it is turned on by hand, its queries go through the tunnel',
  'settings.guard.dot.title': 'Block DNS-over-TLS (port 853)',
  'settings.guard.dot.desc.android':
      'Android in “Private DNS: automatic” mode will fall back to plain DNS. If “Private DNS” is set by hand, sites will stop opening',
  'settings.guard.dot.desc': 'Programs with their own DNS-over-TLS will switch to DNS through the tunnel',
  'settings.guard.strict.title': 'Strict DNS in Windows',
  'settings.guard.strict.desc': 'Stop Windows from asking the DNS of other network adapters around the tunnel',
  'settings.guard.title': 'DNS leak protection',
  'settings.guard.desc.android':
      'Browser DNS and DNS-over-TLS only through the VPN. If “Private DNS” is set by hand in Android, sites will stop opening with the protection on',
  'settings.guard.desc.windows': 'Browser DNS, DNS-over-TLS and the DNS of other Windows network adapters do not go around the tunnel',
  'settings.guard.desc': 'Browser DNS and DNS-over-TLS do not go around the tunnel',
  'settings.guard.partly': 'Not everything is on: {on} of {total}. Turn it on to protect everything',

  // Settings page: the app itself.
  'settings.version.title': 'Version',
  'settings.version.commit': 'commit {commit}',
  'settings.service_version.title': 'Service version',
  'settings.updates.title': 'Updates',
  'settings.updates.install.android': 'Update',
  'settings.updates.install': 'Install now',
  'settings.updates.install.tip': 'The VPN will disconnect during the install and connect again',
  'settings.updates.download': 'Download',
  'settings.updates.check': 'Check now',
  'settings.updates.auto.title': 'Install updates automatically',
  'settings.updates.auto.desc':
      'Once a day CoreShift checks for a new version and installs it by itself while the VPN is off. '
      'If the VPN is on, the install waits.',
  'settings.disclaimer.title': 'Disclaimer',
  'settings.disclaimer.desc': 'A non-commercial program “as is”: servers and subscriptions are provided by your provider',
  'settings.disclaimer.read': 'Read',

  // Settings page: the cores card.
  'settings.cores.title': 'Cores',
  'settings.cores.single': 'One core runs, with no switching on failure',
  'settings.cores.chain': 'If a core stops working, CoreShift switches to the next one',

  // Cores page.
  'cores.title': 'Cores',
  'cores.subtitle': 'The installed cores and the order CoreShift tries them in.',
  'cores.update.installing': 'Installing {latest}…',
  'cores.update.soon': 'Version {latest} will install itself soon',
  'cores.pill.running': 'running',
  'cores.pill.failed': 'failed',
  'cores.item.absent.android': 'not in this build',
  'cores.item.absent': 'not in the cores folder',
  'cores.item.installed': 'installed',
  'cores.item.version': 'version {version}',
  'cores.switch.last': 'At least one core is needed',
  'cores.switch.off': 'Do not use this core',
  'cores.switch.on': 'Use',
  'cores.updates.android': 'Cores update together with the app.',
  'cores.updates': 'CoreShift updates cores itself: it looks for new versions once a day and installs them while the VPN is off.',
  'cores.list.sub.single': 'only one is used',
  'cores.list.sub': 'the preferred one is on top',
  'cores.list.note.single': 'One core is on: {name}. If it stops working, there will be no switching.',
  'cores.list.note':
      'On connect, the first core in the list that supports the server protocol is taken. '
      'Incompatible ones are skipped, the rest become backups.',
  'cores.backup.title': 'Backup core running: {name}',
  'cores.backup.text': '{primary} stopped working{why}. You can switch back right away: it is checked in the background first.',
  'cores.backup.return': 'Return to {primary}',
  'cores.rules.title': 'Switching on failure',
  'cores.rules.return.title': 'Return to the main core',
  'cores.rules.return.before': 'try the main core after',
  'cores.rules.return.after': 'min on the backup',
  'cores.rules.return.stay': 'Stay on the backup core until reconnecting',
  'cores.rules.more': 'Advanced',
  'cores.rules.more.sub': 'when a core counts as not working',
  'cores.rules.health.title': 'Connection check',
  'cores.rules.health.before': 'fail after',
  'cores.rules.health.middle': 'failures in a row, every',
  'cores.rules.health.after': 's',
  'cores.rules.latency.title': 'High latency',
  'cores.rules.latency.before': 'count a response slower than',
  'cores.rules.latency.after': 'ms as a failure',
  'cores.rules.latency.off': 'Slow responses do not count as a failure',
  'cores.rules.url.title': 'Connection check address',
  'cores.rules.url.desc': 'Answers 204 or 200. Fallbacks: Google, Apple',
  'cores.rules.note': 'A core process crash, a rejected config and an unsupported protocol always switch the core.',

  // Proxy mode (without TUN).
  'proxy.system.title': 'System proxy',
  'proxy.system.desc':
      'If “All apps through VPN” will not turn on: CoreShift sets the proxy in the system itself. '
      'Works for browsers and most programs, but not for games or anything that does not understand a proxy',
  'proxy.only.title': 'Proxy without VPN',
  'proxy.only.desc':
      'The VPN does not turn on: only apps set up for the proxy go through CoreShift. '
      'For when the VPN slot is taken by another VPN or a work profile, or only Telegram or a browser is needed',
  'proxy.copied': '{what} copied',
  'proxy.telegram.failed': 'Telegram did not open: is it installed?',
  'proxy.copy': 'Copy',
  'proxy.access.title': 'Proxy for apps',
  'proxy.access.address': 'Address',
  'proxy.access.port': 'Port',
  'proxy.access.login': 'Username',
  'proxy.access.password': 'Password',
  'proxy.access.no_credentials': 'There is no username and password yet: they appear after CoreShift restarts',
  'proxy.access.hide': 'Hide',
  'proxy.access.show': 'Show',
  'proxy.access.telegram': 'Open in Telegram',
  'proxy.access.new_password': 'New password',
  'proxy.access.howto':
      'In a browser or an app with proxy settings choose SOCKS5 (or HTTP), address 127.0.0.1, port {port}, and this username and password. '
      'Other apps bypass CoreShift.',
  'proxy.access.wifi': 'The proxy in Android Wi-Fi settings accepts only HTTP without a password: turn on “HTTP without password” for it.',
  'proxy.open_http.title': 'HTTP without password',
  'proxy.open_http.desc': 'Any app on the phone will be able to use the proxy. Needed only for the proxy in Wi-Fi settings',
  'proxy.open_http.on': 'On: any app on the phone can use the proxy',

  // The journal and the session log: the mode without TUN, the system proxy.
  'proxy.mode.android': 'proxy without VPN',
  'proxy.mode.system': 'system proxy',
  'proxy.mode.only': 'proxy only',
  'proxy.report.applied': 'system proxy is on ({names}): browsers and programs go through {address}; the previous settings are saved',
  'proxy.report.unsupported':
      'system proxy is not set: this desktop is not supported (GNOME or KDE needed). '
      'CoreShift works as a plain proxy: set SOCKS5 or HTTP {address} in your programs',
  'proxy.report.restored': 'system proxy is off, the previous settings are restored ({names})',
  'proxy.report.changed': 'system proxy was already changed by hand ({names}): left as it is',
  'proxy.report.error': 'system proxy: {error}',
  'proxy.report.pending': 'the previous proxy settings will return at the next sign-in or CoreShift start',
  'proxy.err.no_daemon': 'coreshiftd not found',
  'proxy.err.no_answer': 'coreshiftd did not answer',
  'proxy.err.web': 'not available in the browser',

  // Program lists (desktop).
  'apps.direct.title': 'Programs without VPN',
  'apps.direct.sub': 'games, torrents, banks',
  'apps.direct.desc':
      'These programs work directly, as without a VPN, wherever they are installed. Handy for games (lower ping), '
      'torrents and banking apps that refuse connections from abroad.',
  'apps.direct.empty': 'Empty for now: all programs go through the VPN',
  'apps.direct.badge': 'no VPN',
  'apps.direct.remove': 'Back through VPN',
  'apps.proxy.title': 'Programs through VPN',
  'apps.proxy.sub': 'all their traffic',
  'apps.proxy.empty': 'Empty for now',
  'apps.proxy.badge': 'through VPN',
  'apps.proxy.remove': 'Remove from the list',
  'apps.add': 'Add',
  'apps.pick': 'Pick from running',
  'apps.picker.title': 'Running programs',
  'apps.picker.hint.linux': 'Start the program you need if it is not in the list. System programs are not shown.',
  'apps.picker.hint.windows': 'Start the program you need if it is not in the list. Windows system programs are not shown.',
  'apps.picker.search': 'Search',
  'apps.picker.empty': 'Nothing found',
  'apps.picker.refresh': 'Refresh list',
  'apps.done': 'Done',

  // Android: the apps that use the VPN.
  'apps.android.title': 'Apps',
  'apps.android.all': 'All apps',
  'apps.android.all.desc': 'All apps go through the VPN by the rules below',
  'apps.android.exclude': 'Except selected',
  'apps.android.exclude.desc': 'Selected apps work directly, as without a VPN',
  'apps.android.only': 'Only selected',
  'apps.android.only.desc': 'Only they go through the VPN, the rest go direct',
  'apps.android.none.only': 'Nothing selected: all apps go through the VPN for now',
  'apps.android.none': 'Nothing selected',
  'apps.android.chosen': 'Selected: {n}',
  'apps.android.select': 'Select',
  'apps.android.picker.only': 'Through VPN',
  'apps.android.picker.exclude': 'Without VPN',
  'apps.android.picker.search': 'Search apps',
  'apps.android.picker.system': 'Show system apps',
  'apps.android.picker.empty': 'Nothing found',
};
