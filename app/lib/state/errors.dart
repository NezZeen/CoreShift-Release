import '../platform/platform.dart' as platform;

/// Turns the daemon's English errors into Russian a user can act on. The
/// journal keeps the original text; this is for toasts, dialogs and status.
String humanError(String raw) {
  final e = raw.trim();
  if (e.isEmpty) return e;
  final low = e.toLowerCase();
  bool has(String s) => low.contains(s);

  final settings = _settingsErrors(e);
  if (settings != null) return settings;

  final panelMsg = RegExp(r'the panel sent a message instead of servers: (.+)$', caseSensitive: false).firstMatch(e);
  if (panelMsg != null) {
    return 'Панель прислала сообщение вместо серверов: «${panelMsg.group(1)}». '
        'Обычно это лимит устройств, истёкшая или отключённая подписка.';
  }

  final status = RegExp(r'server returned (\d{3})').firstMatch(e);
  if (status != null) {
    final code = int.parse(status.group(1)!);
    return switch (code) {
      401 || 403 => 'Панель отказала в доступе (ошибка $code). Подписка отключена или ссылка неверная.',
      404 => 'Подписка не найдена (ошибка 404). Проверьте ссылку.',
      429 => 'Панель просит подождать: слишком много запросов (ошибка 429).',
      >= 500 => 'Панель временно не работает (ошибка $code). Попробуйте позже.',
      _ => 'Панель ответила ошибкой $code.',
    };
  }

  if (has('fetch subscription')) {
    if (has('no such host')) return 'Не удалось найти адрес панели. Проверьте ссылку и интернет.';
    if (has('timeout') || has('deadline exceeded')) return 'Панель не ответила вовремя. Проверьте интернет или попробуйте позже.';
    if (has('certificate') || has('x509') || has('tls')) return 'Не удалось установить защищённое соединение с панелью (ошибка сертификата).';
    if (has('larger than')) return 'Панель прислала слишком большой ответ — это не похоже на подписку.';
    return 'Не удалось скачать подписку: нет связи с панелью.';
  }

  if (has('servers are already added')) return 'Эти серверы уже есть в списке.';
  if (has('already added')) return 'Эта подписка уже добавлена.';
  if (has('invalid subscription url') || has('subscription url starts with') || has('url cannot contain spaces')) {
    return 'Неверная ссылка на подписку: она должна начинаться с https://';
  }
  if (has('subscription is empty')) return 'Подписка пустая: панель не прислала ни одного сервера.';
  if (has('no usable nodes') || has('unrecognized subscription format')) {
    return 'В подписке нет серверов, которые понимает CoreShift.';
  }
  if (has('no node selected')) return 'Сначала выберите сервер.';
  if (has('is no longer in its subscription')) return 'Выбранный сервер пропал из подписки — выберите другой.';
  if (has('nothing to reconnect')) return 'Переподключать нечего: подключения ещё не было.';
  // The core's local port: taken by another program, or reserved by
  // Windows for Hyper-V, WSL or Docker (netsh int ipv4 show
  // excludedportrange protocol=tcp), which changes after a reboot.
  if (has('already in use') || has('forbidden by its access permissions') || has('only one usage of each socket address')) {
    final port = RegExp(r'127\.0\.0\.1:(\d+)').firstMatch(e)?.group(1) ?? '17890';
    if (platform.isAndroid) {
      return 'Порт $port на телефоне занят другим приложением, скорее всего другим VPN-клиентом. Закройте его и подключитесь снова.';
    }
    return 'Порт $port на этом компьютере занят другой программой (например, другим VPN-клиентом) или зарезервирован Windows '
        'для Hyper-V, WSL или Docker. Закройте другой VPN-клиент; если не поможет — перезагрузите компьютер.';
  }
  // Android's VpnService (engine/mobile).
  if (has('android turned the vpn off')) return 'Android выключил VPN: запущен другой VPN-клиент или CoreShift отключён в настройках системы.';
  if (has('vpn permission is not granted')) return 'Нет разрешения на VPN. Нажмите «Подключить» и разрешите запрос Android.';
  if (has('the vpn service did not start')) return 'Android не запустил VPN. Попробуйте ещё раз; если повторится — перезапустите CoreShift.';
  if (has('the network reported no dns servers')) return 'Нет сети: телефон не получил адреса DNS. Проверьте Wi-Fi или мобильный интернет.';
  if (has('every compatible core failed')) return 'Сервер не отвечает ни через одно ядро. Попробуйте другой сервер.';
  if (has('tun mode needs the sing-box')) return 'Для режима «Все приложения» нужно ядро sing-box.';
  if (has('tun layer stopped')) return 'Сетевой адаптер VPN неожиданно остановился.';
  if (has('start tun layer')) return 'Не удалось создать сетевой адаптер VPN. Подробности в журнале.';
  if (has('redirect system dns')) return 'Не удалось перенастроить DNS Windows. Подробности в журнале.';
  if (has('resolve server')) return 'Не удалось узнать адрес сервера VPN: DNS не отвечает.';

  // Cores: returning to the primary, updates.
  if (has('already on the primary')) return 'Уже работает основное ядро.';
  if (has('not connected')) return 'Нет подключения.';
  if (has('switching right now')) return 'Ядро сейчас переключается, попробуйте через пару секунд.';
  if (has('another core is being updated')) return 'Уже идёт обновление другого ядра.';
  if (has('checksum mismatch')) return 'Скачанный файл повреждён: не совпала контрольная сумма. Попробуйте ещё раз.';
  if (has('does not start, kept the old one')) return 'Новая версия ядра не запустилась, оставлена прежняя.';
  // Updates of CoreShift itself.
  if (has('no token for the private releases')) return 'Эта сборка собрана без доступа к обновлениям: новые версии ставьте установщиком.';
  if (has('releases token is invalid or expired')) return 'Доступ к обновлениям истёк. Установите новую версию CoreShift вручную.';
  if (has('no release found, or the token has no access')) return 'Обновлений не найдено: версии ещё не выложены или нет доступа к ним.';
  if (has('update signature')) return 'Обновление отклонено: его подпись не совпадает. Устанавливаются только проверенные версии.';
  if (has('did not install')) return 'Обновление не установилось. Попробуйте кнопкой «Установить сейчас» или поставьте версию вручную.';
  if (has('downloaded update changed or is gone')) return 'Скачанное обновление повреждено, оно будет скачано заново.';
  if (has('start the installer')) return 'Не удалось запустить установку обновления.';
  if (has('check for updates')) {
    if (has('403') || has('429')) return 'GitHub временно ограничил проверки обновлений. Попробуйте через час.';
    return 'Не удалось проверить обновления: нет связи с GitHub.';
  }
  if (has('download:')) return 'Не удалось скачать обновление ядра: нет связи с GitHub.';
  if (has('no release build for')) return 'Для этой системы нет готовой сборки ядра.';
  return e;
}

/// The store reports every invalid setting on its own line, e.g.
/// `routing.direct_domains: "bad domain" is not a domain`. Returns null
/// when no line is about a list the user typed.
String? _settingsErrors(String e) {
  final rules = <(RegExp, String Function(Match))>[
    (RegExp(r'"(.*)": write internationalized names in punycode'), (m) => '«${m[1]}»: русские домены пишите в punycode, например .рф — это xn--p1ai.'),
    (RegExp(r'"(.*)" is not a domain'), (m) => '«${m[1]}» — не похоже на адрес сайта.'),
    (RegExp(r'"(.*)" is too long'), (m) => '«${m[1]}» — слишком длинный адрес.'),
    (RegExp(r'"(.*)" is not an address or subnet'), (m) => '«${m[1]}» — не похоже на IP-адрес или подсеть.'),
    (RegExp('"(.*)" overlaps the tunnel\'s own addresses'), (m) => '«${m[1]}» пересекается со служебными адресами VPN.'),
    (RegExp(r'"(.*)" is not a program name'), (m) => '«${m[1]}» — не похоже на имя программы.'),
    (RegExp(r'at most (\d+) (apps|entries)'), (m) => 'В списке может быть не больше ${m[1]} записей.'),
  ];
  var matched = false;
  final lines = [
    for (final line in e.split('\n'))
      if (line.trim().isNotEmpty)
        () {
          for (final (re, text) in rules) {
            final m = re.firstMatch(line);
            if (m != null) {
              matched = true;
              return text(m);
            }
          }
          return line;
        }(),
  ];
  return matched ? lines.toSet().join('\n') : null;
}
