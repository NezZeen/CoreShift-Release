import '../l10n/engine_strings.dart';
import '../l10n/strings.dart';
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
String get insecureLinkWarning => tr('err.insecureLink');

/// A failed speed test. The service names the phase first, "download: …",
/// which [humanError] would take for a core update that could not download.
/// The test goes to speedtest.net, or to Cloudflare when speedtest.net
/// cannot be reached; an error that names neither phase means both failed.
String speedTestError(String raw) {
  final e = raw.trim();
  final m = RegExp(r'^(download|upload): (.*)$', dotAll: true).firstMatch(e);
  if (m == null) {
    final low = e.toLowerCase();
    if (low.contains('speed test server unreachable')) return tr('err.speed.unreachable');
    if (low.contains('context canceled')) return tr('err.speed.canceled');
    return humanError(e);
  }
  final what = m[1] == 'download' ? tr('err.speed.what.download') : tr('err.speed.what.upload');
  final why = m[2]!.toLowerCase();
  if (why.contains('context canceled')) return tr('err.speed.canceled');
  final reason = why.contains('403') || why.contains('429')
      ? tr('err.speed.reason.limited')
      : why.contains('timeout') || why.contains('deadline') || why.contains('nothing went through')
      ? tr('err.speed.reason.timeout')
      : tr('err.speed.reason.broken');
  return tr('err.speed.failed', {'what': what, 'reason': reason});
}

/// Turns the daemon's English errors into the app's language a user can act
/// on. The journal keeps the original text; this is for toasts, dialogs and
/// status.
String humanError(String raw) {
  final e = raw.trim();
  if (e.isEmpty) return e;
  final low = e.toLowerCase();
  bool has(String s) => low.contains(s);

  final settings = _settingsErrors(e);
  if (settings != null) return settings;

  final panelMsg = RegExp(r'the panel sent a message instead of servers: (.+)$', caseSensitive: false).firstMatch(e);
  if (panelMsg != null) return tr('err.panelMessage', {'msg': panelMsg.group(1)});

  final status = RegExp(r'server returned (\d{3})').firstMatch(e);
  if (status != null) {
    final code = int.parse(status.group(1)!);
    return switch (code) {
      401 || 403 => tr('err.panel.denied', {'code': code}),
      404 => tr('err.panel.notFound'),
      429 => tr('err.panel.tooMany'),
      >= 500 => tr('err.panel.down', {'code': code}),
      _ => tr('err.panel.other', {'code': code}),
    };
  }

  if (has('fetch subscription')) {
    if (has('no such host')) return tr('err.fetch.noHost');
    if (has('timeout') || has('deadline exceeded')) return tr('err.fetch.timeout');
    if (has('certificate') || has('x509') || has('tls')) return tr('err.fetch.tls');
    if (has('larger than')) return tr('err.fetch.tooLarge');
    return tr('err.fetch.noLink');
  }

  if (has('servers are already added')) return tr('err.serversAlready');
  if (has('already added')) return tr('err.subAlready');
  if (has('invalid subscription url') || has('subscription url starts with') || has('url cannot contain spaces')) {
    return tr('err.badSubUrl');
  }
  if (has('subscription is empty')) return tr('err.subEmpty');
  if (has('no usable nodes') || has('unrecognized subscription format')) {
    return tr('err.noUsableNodes');
  }
  if (has('no node selected')) return tr('err.noNodeSelected');
  if (has('is no longer in its subscription')) return tr('err.nodeGone');
  if (has('nothing to reconnect')) return tr('err.nothingToReconnect');
  // The core's local port: taken by another program, or reserved by
  // Windows for Hyper-V, WSL or Docker (netsh int ipv4 show
  // excludedportrange protocol=tcp), which changes after a reboot.
  if (has('already in use') || has('forbidden by its access permissions') || has('only one usage of each socket address')) {
    final port = RegExp(r'127\.0\.0\.1:(\d+)').firstMatch(e)?.group(1) ?? '17890';
    if (platform.isAndroid) return tr('err.port.android', {'port': port});
    if (platform.isLinux) return tr('err.port.linux', {'port': port});
    return tr('err.port.windows', {'port': port});
  }
  // Android's VpnService (engine/mobile).
  if (has('android turned the vpn off')) return tr('err.android.off');
  if (has('vpn permission is not granted')) return tr('err.android.noPermission');
  if (has('the vpn service did not start')) return tr('err.android.notStarted');
  if (has('the network reported no dns servers')) return tr('err.android.noDns');
  if (has('every compatible core failed')) return tr('err.coresFailed');
  if (has('tun mode needs the sing-box')) return tr('err.tunNeedsSingbox');
  if (has('tun layer stopped')) return tr('err.tunStopped');
  if (has('start tun layer')) return tr('err.tunStart');
  if (has('redirect system dns')) return tr('err.dnsRedirect', {'os': platform.isLinux ? tr('err.os.system') : 'Windows'});
  // Services before 0.7.2; later ones say it in Russian themselves.
  if (has('resolve server')) {
    if (has('no such host')) return tr('err.resolve.noHost');
    return tr('err.resolve.noAnswer');
  }

  // Cores: returning to the primary, updates.
  if (has('already on the primary')) return tr('err.core.alreadyPrimary');
  if (has('not connected')) return tr('err.core.notConnected');
  if (has('switching right now')) return tr('err.core.switching');
  if (has('another core is being updated')) return tr('err.core.otherUpdating');
  if (has('checksum mismatch')) return tr('err.core.checksum');
  if (has('does not start, kept the old one')) return tr('err.core.notStarted');
  // Updates of CoreShift itself.
  if (has('no token for the private releases')) return tr('err.update.noToken');
  if (has('releases token is invalid or expired')) return tr('err.update.tokenExpired');
  if (has('no release found, or the token has no access')) return tr('err.update.noRelease');
  // GitHub out of reach, and its mirror on GitLab too (appupdate.go).
  if (has('; gitlab: ')) return tr('err.update.noGithubGitlab');
  if (has('github rate limit')) return tr('err.update.githubRateLater');
  if (has('update signature')) return tr('err.update.signature');
  if (has('did not install')) return tr('err.update.notInstalled');
  if (has('downloaded update changed or is gone')) return tr('err.update.changed');
  if (has('start the installer')) return tr('err.update.installer');
  if (has('check for updates')) {
    if (has('no release has')) return tr('err.update.noneForSystem');
    if (has('403') || has('429')) return tr('err.update.githubRateHour');
    return tr('err.update.noGithub');
  }
  if (has('download:')) return tr('err.update.coreDownload');
  if (has('no release build for')) return tr('err.update.noCoreBuild');
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
        toast: tr('err.vpnRefused.unasked.toast'),
        title: tr('err.vpnRefused.unasked.title'),
        text: tr('err.vpnRefused.unasked.text'),
        journal: tr('err.vpnRefused.unasked.journal'),
      )
    : (
        toast: tr('err.vpnRefused.asked.toast'),
        title: tr('err.vpnRefused.asked.title'),
        text: tr('err.vpnRefused.asked.text'),
        journal: tr('err.vpnRefused.asked.journal'),
      );

/// What the home page says when Android turned the VPN off itself
/// ("android turned the vpn off", engine/mobile): another app took it.
({String title, String text}) get vpnRevokedText => (title: tr('err.vpnRevoked.title'), text: tr('err.vpnRevoked.text'));

/// A service's error as the journal shows it: in the app's language where
/// [humanError] knows it, the service's own Russian as it is. Where the
/// translation sends to the journal for the cause, the original follows it.
String journalError(String raw) {
  final e = raw.trim();
  if (_russianLine.hasMatch(e)) return e;
  final human = humanError(e);
  final more = ' ${tr('err.moreInJournal')}';
  if (human != e && human.endsWith(more)) return '${human.substring(0, human.length - more.length)} ${tr('err.details', {'raw': e})}';
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
  if (low.contains('403') || low.contains('429') || low.contains('rate limit')) return tr('err.coreCheck.limited');
  if (low.contains('check for updates')) {
    if (low.contains('timeout') || low.contains('deadline exceeded')) return tr('err.coreCheck.timeout');
    return tr('err.coreCheck.noLink');
  }
  if (low.contains('no release build for')) return tr('err.coreCheck.noBuild');
  final text = humanError(raw);
  if (text.isEmpty) return text;
  // A sentence of its own: lower-case and without the full stop. A first
  // word with a capital inside (GitHub, CoreShift, DNS) is a name: kept.
  final s = text.endsWith('.') ? text.substring(0, text.length - 1) : text;
  if (RegExp(r'[A-Z]').hasMatch(s.split(' ').first.substring(1))) return s;
  return s[0].toLowerCase() + s.substring(1);
}

/// The one journal line for automatic checks of the cores that keep
/// failing: [failed] maps each core to the service's error, '' meaning
/// the service could not be asked at all.
String coreCheckFailedText(Map<String, String> failed, int times, {required bool retryInHour}) {
  final cores = failed.keys.where((k) => k.isNotEmpty).toList();
  final reasons = failed.values.map(coreCheckReason).toSet().join('; ');
  return tr('err.coreCheck.failed', {
    'cores': cores.isEmpty ? '' : ' (${cores.join(', ')})',
    'times': times,
    'reasons': reasons,
    'when': retryInHour ? tr('err.coreCheck.inHour') : tr('err.coreCheck.tomorrow'),
  });
}

/// The store reports every invalid setting on its own line, e.g.
/// `routing.direct_domains: "bad domain" is not a domain`. Returns null
/// when no line is about a list the user typed.
String? _settingsErrors(String e) {
  final rules = <(RegExp, String Function(Match))>[
    (RegExp(r'"(.*)": write internationalized names in punycode'), (m) => tr('err.set.punycode', {'v': m[1]})),
    (RegExp(r'"(.*)" is not a domain'), (m) => tr('err.notDomain', {'v': m[1]})),
    (RegExp(r'"(.*)" is too long'), (m) => tr('err.set.tooLong', {'v': m[1]})),
    (RegExp(r'"(.*)" is not an address or subnet'), (m) => tr('err.notAddress', {'v': m[1]})),
    (RegExp('"(.*)" overlaps the tunnel\'s own addresses'), (m) => tr('err.set.overlap', {'v': m[1]})),
    (RegExp(r'"(.*)" is not a program name'), (m) => tr('err.set.notProgram', {'v': m[1]})),
    (RegExp(r'at most (\d+) (apps|entries)'), (m) => tr('err.set.atMostEntries', {'n': m[1]})),
    // The user's rules and where their categories come from.
    (RegExp(r'"(.*)": (category name may have only|category name has|empty category or attribute)'), (m) => tr('err.set.categoryChars', {'v': m[1]})),
    (RegExp(r'"(.*)": no category name'), (m) => tr('err.set.noCategory', {'v': m[1]})),
    (RegExp(r'"(.*)": category name is longer than (\d+)'), (m) => tr('err.set.categoryLong', {'v': m[1], 'n': m[2]})),
    (RegExp(r'"(.*)": geoip categories have no attributes'), (m) => tr('err.set.geoipAttrs', {'v': m[1]})),
    (RegExp(r'"(.*)": "(.*)" is not an action'), (m) => tr('err.set.badAction', {'v': m[1], 'action': m[2]})),
    (RegExp(r'at most (\d+) categories'), (m) => tr('err.set.atMostCategories', {'n': m[1]})),
    (RegExp(r'routing\.geo\.\w+: link must start with https'), (m) => tr('err.set.linkHttps')),
    // "{name}" is the placeholder of the link, said as it is.
    (RegExp(r'routing\.geo\.\w+: link has \{name\} more than once'), (m) => tr('err.set.linkNameOnce', {'name': '{name}'})),
    (RegExp(r'routing\.geo\.\w+: link must not have a user name'), (m) => tr('err.set.linkUser')),
    (RegExp(r'routing\.geo\.\w+: link (has spaces|is longer)'), (m) => tr('err.set.linkBad')),
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
