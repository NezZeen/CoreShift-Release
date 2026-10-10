import '../l10n/engine_strings.dart';
import '../platform/platform.dart' as platform;

/// An engine's error for a toast, a dialog or the status: its [code], worded
/// in the app's language, as a sentence; [humanError] of its [raw] text when
/// it has no code the app knows.
String humanCodedError(String code, Map<String, dynamic> args, String raw) {
  final t = code.isEmpty ? null : engineMessage(code, args);
  return t == null ? humanError(raw) : sentence(t);
}

/// [humanCodedError] for the journal: as the engine says it, else
/// [journalError].
String journalCodedError(String code, Map<String, dynamic> args, String raw) {
  final t = code.isEmpty ? null : engineMessage(code, args);
  return t ?? journalError(raw);
}

/// [s] with a capital and a full stop.
String sentence(String s) => s.isEmpty ? s : '${s[0].toUpperCase()}${s.substring(1)}${s.endsWith('.') ? '' : '.'}';

/// Said when a subscription with a plain http:// link is added.
const insecureLinkWarning = 'Ссылка без шифрования, токен виден в сети. Попросите у поставщика ссылку https://';

/// A failed speed test. The service names the phase first, "download: …",
/// which [humanError] would take for a core update that could not download.
/// The test goes to speedtest.net, or to Cloudflare when speedtest.net
/// cannot be reached; an error that names neither phase means both failed.
String speedTestError(String raw) {
  final e = raw.trim();
  final m = RegExp(r'^(download|upload): (.*)$', dotAll: true).firstMatch(e);
  if (m == null) {
    final low = e.toLowerCase();
    if (low.contains('speed test server unreachable')) {
      return 'Не удалось замерить скорость: нет связи ни с speedtest.net, ни с Cloudflare. Проверьте подключение к интернету.';
    }
    if (low.contains('context canceled')) return 'Тест скорости прерван.';
    return humanError(e);
  }
  final what = m[1] == 'download' ? 'загрузку' : 'отдачу';
  final why = m[2]!.toLowerCase();
  if (why.contains('context canceled')) return 'Тест скорости прерван.';
  final reason = why.contains('403') || why.contains('429')
      ? 'сервер теста ограничил запросы, попробуйте через несколько минут'
      : why.contains('timeout') || why.contains('deadline') || why.contains('nothing went through')
      ? 'сервер теста не ответил вовремя'
      : 'связь с сервером теста оборвалась';
  return 'Не удалось замерить $what: $reason.';
}

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
    if (platform.isLinux) {
      return 'Порт $port на этом компьютере занят другой программой, например другим VPN-клиентом. '
          'Закройте её и подключитесь снова; кто занял порт, покажет «ss -ltnp».';
    }
    return 'Порт $port на этом компьютере занят другой программой (например, другим VPN-клиентом) или зарезервирован Windows '
        'для Hyper-V, WSL или Docker. Закройте другой VPN-клиент; если не поможет — перезагрузите компьютер.';
  }
  // Android's VpnService (engine/mobile).
  if (has('android turned the vpn off')) return 'Android выключил VPN: запущен другой VPN-клиент или CoreShift отключён в настройках системы.';
  if (has('vpn permission is not granted')) {
    return 'Нет разрешения на VPN. Нажмите «Подключить» и разрешите запрос Android; '
        'если запрос не появляется, выключите «Постоянную VPN» у другого приложения в настройках VPN.';
  }
  if (has('the vpn service did not start')) return 'Android не запустил VPN. Попробуйте ещё раз; если повторится — перезапустите CoreShift.';
  if (has('the network reported no dns servers')) return 'Нет сети: телефон не получил адреса DNS. Проверьте Wi-Fi или мобильный интернет.';
  if (has('every compatible core failed')) return 'Сервер не отвечает ни через одно ядро. Попробуйте другой сервер.';
  if (has('tun mode needs the sing-box')) return 'Для режима «Все приложения» нужно ядро sing-box.';
  if (has('tun layer stopped')) return 'Сетевой адаптер VPN неожиданно остановился.';
  if (has('start tun layer')) return 'Не удалось создать сетевой адаптер VPN. Подробности в журнале.';
  if (has('redirect system dns')) return 'Не удалось перенастроить DNS ${platform.isLinux ? 'системы' : 'Windows'}. Подробности в журнале.';
  // Services before 0.7.2; later ones say it in Russian themselves.
  if (has('resolve server')) {
    if (has('no such host')) return 'Адрес сервера VPN не найден в DNS. Проверьте интернет; если он есть — обновите подписку.';
    return 'Не удалось узнать адрес сервера VPN: DNS не отвечает.';
  }

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
  // GitHub out of reach, and its mirror on GitLab too (appupdate.go).
  if (has('; gitlab: ')) return 'Не удалось проверить обновления: нет связи ни с GitHub, ни с зеркалом на GitLab. CoreShift проверит ещё раз позже.';
  if (has('github rate limit')) return 'GitHub временно ограничил проверки обновлений с этого адреса. CoreShift проверит ещё раз позже.';
  if (has('update signature')) return 'Обновление отклонено: его подпись не совпадает. Устанавливаются только проверенные версии.';
  if (has('did not install')) return 'Обновление не установилось. Попробуйте кнопкой «Установить сейчас» или поставьте версию вручную.';
  if (has('downloaded update changed or is gone')) return 'Скачанное обновление повреждено, оно будет скачано заново.';
  if (has('start the installer')) return 'Не удалось запустить установку обновления.';
  if (has('check for updates')) {
    if (has('no release has')) return 'Обновлений пока нет: для этой системы версии ещё не выкладывались.';
    if (has('403') || has('429')) return 'GitHub временно ограничил проверки обновлений. Попробуйте через час.';
    return 'Не удалось проверить обновления: нет связи с GitHub.';
  }
  if (has('download:')) return 'Не удалось скачать обновление ядра: нет связи с GitHub.';
  if (has('no release build for')) return 'Для этой системы нет готовой сборки ядра.';
  // Said in Russian by the service, as a line of its journal: a sentence here.
  if (_russianLine.hasMatch(e)) return '${e[0].toUpperCase()}${e.substring(1)}${e.endsWith('.') ? '' : '.'}';
  return e;
}

final _russianLine = RegExp('^[а-яё]');

/// Android refused CoreShift the VPN (VpnService.prepare, MainActivity.kt).
/// [unasked]: at once, without its request on the screen. Android does so
/// while another app is the "always-on" VPN («Постоянная VPN»): it gives the
/// VPN to no one else and shows nothing, so pressing «Подключить» again did
/// nothing either. The toast, the home page's notice ([title], [text]) and
/// the journal's line, which says how to fix it.
({String toast, String title, String text, String journal}) vpnRefusedText({required bool unasked}) => unasked
    ? (
        toast: 'Android отказал в VPN: похоже, у другого приложения включена «Постоянная VPN»',
        title: 'Android отказал в VPN, не спросив',
        text:
            'Так бывает, когда у другого VPN-приложения включена «Постоянная VPN»: пока она включена, Android не даёт VPN никому другому. '
            'Откройте настройки VPN, выключите её у того приложения и нажмите «Подключить» снова.',
        journal:
            'Android отказал в VPN, не показав запрос: у другого VPN-приложения включена «Постоянная VPN» (Always-on) '
            'или VPN для CoreShift запрещён в настройках системы. Откройте настройки VPN, выключите «Постоянную VPN» '
            'у другого приложения и подключитесь снова',
      )
    : (
        toast: 'Android не разрешил VPN: без этого подключиться нельзя',
        title: 'Android не разрешил VPN',
        text:
            'Без разрешения подключиться нельзя: нажмите «Подключить» и выберите «ОК» в запросе Android. '
            'Если запрос не появляется, проверьте в настройках VPN, не включена ли «Постоянная VPN» у другого приложения.',
        journal:
            'Android не разрешил VPN: запрос отклонён. Нажмите «Подключить» и разрешите запрос; если он не появляется, '
            'выключите «Постоянную VPN» у другого приложения в настройках VPN',
      );

/// What the home page says when Android turned the VPN off itself
/// ("android turned the vpn off", engine/mobile): another app took it.
const vpnRevokedText = (
  title: 'Android выключил VPN',
  text:
      'Другое VPN-приложение забрало VPN себе, или CoreShift отключён в настройках системы. '
      'Если у другого приложения включена «Постоянная VPN», выключите её в настройках VPN и подключитесь снова.',
);

/// A service's error as the journal shows it: in Russian where [humanError]
/// knows it, the service's own Russian as it is. Where the translation sends
/// to the journal for the cause, the original follows it.
String journalError(String raw) {
  final e = raw.trim();
  if (_russianLine.hasMatch(e)) return e;
  final human = humanError(e);
  const more = ' Подробности в журнале.';
  if (human != e && human.endsWith(more)) return '${human.substring(0, human.length - more.length)} Подробности: $e';
  return human;
}

/// Whether a core's update check failed for a reason that passes: no
/// network, GitHub not answering or limiting checks, the service not
/// reachable. Not: the release itself, such as no build for this system.
bool coreCheckTemporary(String raw) {
  final low = raw.toLowerCase();
  // Reaching GitHub, or what it answered (coreupdate.Latest).
  if (low.contains('check for updates')) return true;
  const release = ['no release build for', 'release has no', 'not a version', 'not where the project publishes', 'gives no sha-256', 'unknown core'];
  return !release.any(low.contains);
}

/// Why a core's update check failed, to end a sentence of the journal that
/// already says what failed: "нет связи с GitHub", not "Не удалось
/// проверить обновления: нет связи с GitHub.".
String coreCheckReason(String raw) {
  final low = raw.toLowerCase();
  if (low.contains('403') || low.contains('429') || low.contains('rate limit')) return 'GitHub временно ограничил проверки';
  if (low.contains('check for updates')) {
    if (low.contains('timeout') || low.contains('deadline exceeded')) return 'GitHub не ответил вовремя';
    return 'нет связи с GitHub';
  }
  if (low.contains('no release build for')) return 'для этой системы нет готовой сборки';
  final text = humanError(raw);
  if (text.isEmpty) return text;
  // A sentence of its own: lower-case and without the full stop.
  final s = text.endsWith('.') ? text.substring(0, text.length - 1) : text;
  return s[0].toLowerCase() + s.substring(1);
}

/// The one journal line for automatic checks of the cores that keep
/// failing: [failed] maps each core to the service's error, '' meaning
/// the service could not be asked at all.
String coreCheckFailedText(Map<String, String> failed, int times, {required bool retryInHour}) {
  final cores = failed.keys.where((k) => k.isNotEmpty).toList();
  final reasons = failed.values.map(coreCheckReason).toSet().join('; ');
  final word = times % 10 >= 2 && times % 10 <= 4 && (times % 100 < 12 || times % 100 > 14) ? 'раза' : 'раз';
  return 'не удалось проверить обновления${cores.isEmpty ? '' : ' (${cores.join(', ')})'} $times $word подряд: $reasons. '
      'Следующая попытка ${retryInHour ? 'через час' : 'завтра'}';
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
    // The user's rules and where their categories come from.
    (
      RegExp(r'"(.*)": (category name may have only|category name has|empty category or attribute)'),
      (m) => '«${m[1]}»: в имени категории бывают только латинские буквы, цифры и !@._-',
    ),
    (RegExp(r'"(.*)": no category name'), (m) => '«${m[1]}»: укажите категорию, например geosite:youtube.'),
    (RegExp(r'"(.*)": category name is longer than (\d+)'), (m) => '«${m[1]}»: имя категории длиннее ${m[2]} знаков.'),
    (RegExp(r'"(.*)": geoip categories have no attributes'), (m) => '«${m[1]}»: у категорий geoip нет атрибутов (@…).'),
    (RegExp(r'"(.*)": "(.*)" is not an action'), (m) => '«${m[1]}»: неизвестное действие «${m[2]}».'),
    (RegExp(r'at most (\d+) categories'), (m) => 'Категорий в правилах может быть не больше ${m[1]}.'),
    (RegExp(r'routing\.geo\.\w+: link must start with https'), (m) => 'Ссылка на базы должна начинаться с https://'),
    (RegExp(r'routing\.geo\.\w+: link has \{name\} more than once'), (m) => '{name} должно быть в ссылке на базы один раз.'),
    (RegExp(r'routing\.geo\.\w+: link must not have a user name'), (m) => 'В ссылке на базы не должно быть имени и пароля.'),
    (RegExp(r'routing\.geo\.\w+: link (has spaces|is longer)'), (m) => 'Ссылка на базы неверна: пробелы или больше 2048 знаков.'),
    // Said in Russian already (the demo).
    (RegExp(r'^routing\.[a-z_.]+: ([А-Яа-яЁё«{].*)$'), (m) => m[1]!),
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
