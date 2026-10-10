part of '../app_state.dart';

/// What the cores printed this long before a failure, and after it, goes
/// into the copied journal whatever its level.
const _nearFailureBefore = Duration(seconds: 10);
const _nearFailureAfter = Duration(seconds: 2);

/// A warning or an error in the formats of Xray ("[Warning]", "[Error]"),
/// sing-box ("WARN", "ERROR", "FATAL") and mihomo ("level=warning"); the
/// Go runtime's "panic:" too.
final _outputWarnRe = RegExp(r'\[(warning|error)\]|\b(warn|warning|error|fatal|panic)\b|level=(warn|warning|error|fatal)', caseSensitive: false);
bool _outputWarns(String line) => _outputWarnRe.hasMatch(line);

/// What a core prints every time it starts: its banner, the config it
/// reads, that it started. Xray says the last as a warning.
final _startupRes = [
  RegExp(r'^Xray \S+ \(Xray, Penetrates Everything\.\)'),
  RegExp(r'^A unified platform for anti-censorship\.?$'),
  RegExp(r'infra/conf/serial: Reading config:'),
  RegExp(r'core: Xray \S+ started'),
  RegExp(r'sing-box started \('),
  RegExp(r'Start initial configuration in progress|Initial configuration complete'),
];
bool _startupLine(String line) => _startupRes.any((re) => re.hasMatch(line));

/// The level of a line a core or the TUN layer printed. The service spells
/// it as sing-box does at the start of the line, "ERROR …", "WARN …", for
/// every core; an older service passed Xray's "[Warning]" and
/// mihomo's "level=warning" on as printed. Start-up lines are no news, even
/// the one Xray prints as a warning.
final _outputErrRe = RegExp(r'^(ERROR|FATAL|PANIC)\b|^panic:|\[Error\]|level=(error|fatal|panic)\b');
final _outputWarnLevelRe = RegExp(r'^WARN\b|\[Warning\]|level=warn(ing)?\b');
LogLevel _outputLevel(String line) {
  if (_outputErrRe.hasMatch(line)) return LogLevel.err;
  if (_outputWarnLevelRe.hasMatch(line)) return _startupLine(line) ? LogLevel.info : LogLevel.warn;
  return LogLevel.info;
}

/// What the app does on the user's behalf: connecting, subscriptions, settings,
/// cores, the leak test. Kept apart from the state it works on.
extension AppStateActions on AppState {
  // ---------------------------------------------------------------- actions

  Future<void> toggleConnect() => status.active ? disconnect() : connect();

  /// The first lines of a copied journal: which versions, which system and
  /// how CoreShift is set up. No servers' addresses or subscription links.
  List<String> diagnosticsHeader() {
    String on(String path) => setting(path, false) ? 'да' : 'нет';
    final cores = setting('cores.mode', 'auto') == 'manual'
        ? 'вручную ${setting('cores.manual', '')}'
        : 'автосвап ${(settings['cores']?['priority'] as List?)?.join(' → ') ?? ''}';
    return [
      'CoreShift: приложение ${version.label} (${version.commit.isEmpty ? '—' : version.commit}), '
          'служба ${info.buildVersion.label} (${info.commit.isEmpty ? '—' : info.commit})',
      'Система: ${platform.osDescription}',
      'Режим: ${setting('tun', false) ? 'все приложения (TUN)' : _proxyModeName}; ядра: $cores; '
          'маршруты: ${setting('routing.mode', 'all') == 'selected' ? 'только выбранное' : 'всё через VPN'}; '
          'Россия напрямую: ${on('routing.russia_direct')}; IPv6: ${on('ipv6')}; fake-IP: ${on('dns.fake_ip')}',
      'Сейчас: ${AppState._stateText(status.state.name)}${status.core.isEmpty ? '' : ' через ${status.core}'}'
          '${selection.name.isEmpty ? '' : ', сервер «${selection.name}»'}',
      '',
    ];
  }

  /// The journal for support, on the clipboard and as a file to send: a
  /// desktop saves it to Downloads, Android to Downloads/CoreShift and
  /// opens «Отправить». Returns where the file is, null when none was saved.
  /// [before] goes first, a checkup's report; [kind] names the file.
  Future<String?> copyJournal({List<String> before = const [], String kind = 'log'}) async {
    final text = [...before, ...journalForSupport()].join('\n');
    // Not waited for: the file need not wait on the clipboard.
    unawaited(Clipboard.setData(ClipboardData(text: text)));
    final save = journalSaver;
    if (save == null) return null;
    final t = DateTime.now();
    String two(int n) => n.toString().padLeft(2, '0');
    final name = 'CoreShift-${version.version}-$kind-${t.year}-${two(t.month)}-${two(t.day)}_${two(t.hour)}-${two(t.minute)}.txt';
    try {
      return await save(name, '$text\n');
    } catch (e) {
      toast('Журнал скопирован, но файл не сохранился: ${humanError('$e')}', ToastKind.err);
      return null;
    }
  }

  /// The journal as «Копировать» puts it on the clipboard for support: the
  /// header, every event, and of what the cores and the TUN layer print
  /// themselves only warnings and errors, plus everything they printed
  /// around a failure. Their start-up lines are left out: the banner, the
  /// config's path, "started"; the header has the versions.
  List<String> journalForSupport() {
    final failures = [
      for (final l in logs)
        if (!l.output && (l.level == LogLevel.err || l.level == LogLevel.swap)) l.time,
    ];
    // The journal is in time order: the failures are looked at from the
    // first that has not ended its window yet, not from the start each time.
    var next = 0;
    bool nearFailure(DateTime t) {
      while (next < failures.length && t.isAfter(failures[next].add(_nearFailureAfter))) {
        next++;
      }
      return next < failures.length && !t.isBefore(failures[next].subtract(_nearFailureBefore));
    }

    String two(int n) => n.toString().padLeft(2, '0');
    String time(DateTime t) => '${two(t.hour)}:${two(t.minute)}:${two(t.second)}';

    return [
      ...diagnosticsHeader(),
      for (final l in logs)
        if (!l.output || (!_startupLine(l.message) && (_outputWarns(l.message) || nearFailure(l.time))))
          '${time(l.time)}  ${l.source}  ${redactForSupport(l.message)}',
    ];
  }

  /// Notes what the user did, so a journal copied from another computer
  /// says why the connection changed.
  void _logAction(String text) => _log(DateTime.now(), 'действие', text, LogLevel.info);

  /// Android asks the user once before the app may run a VPN. A refusal goes
  /// into the journal with what to do, and the home page offers Android's VPN
  /// settings ([vpnBlocked]): with another app as the "always-on" VPN,
  /// Android refuses at once without asking, and each press of «Подключить»
  /// used to leave only "действие подключить" in the journal.
  Future<bool> _vpnAllowed() async {
    if (!setting('tun', false)) return true;
    final ask = vpnConsent ?? (backend is DemoBackend ? null : platform.prepareVpn);
    if (ask == null) return true;
    final consent = await ask();
    if (consent == platform.VpnConsent.granted) {
      vpnRefusal = null;
      return true;
    }
    final text = vpnRefusedText(unasked: consent == platform.VpnConsent.unasked);
    _log(DateTime.now(), 'VPN', text.journal, LogLevel.err);
    vpnRefusal = consent;
    toast(text.toast, ToastKind.err);
    return false;
  }

  /// Opens Android's VPN settings, where another app's "always-on" VPN is
  /// turned off.
  Future<void> openVpnSettings() async {
    if (await (vpnSettingsOpener ?? platform.openVpnSettings)()) return;
    toast('Не удалось открыть настройки VPN. Откройте их сами: «Настройки» → «Сеть и интернет» или «Подключение и общий доступ» → «VPN»', ToastKind.err);
  }

  /// Puts away the home page's notice of a refused VPN.
  void dismissVpnRefusal() {
    vpnRefusal = null;
    _notify();
  }

  /// [from] says where the user asked, when not in the window: "трей".
  Future<void> connect({String? subscription, String? fingerprint, String? name, String from = ''}) async {
    noteRecent(subscription ?? selection.subscription, fingerprint ?? selection.fingerprint);
    busy = true;
    final where = from.isEmpty ? '' : ' ($from)';
    _logAction(name != null && name.isNotEmpty ? 'подключить: $name$where' : 'подключить${selection.name.isEmpty ? '' : ': ${selection.name}'}$where');
    _notify();
    if (!await _vpnAllowed()) {
      busy = false;
      _notify();
      return;
    }
    await _act(() async {
      final body = subscription != null ? {'subscription': subscription, 'fingerprint': fingerprint, 'name': ?name} : null;
      status = Status.fromJson(await backend.call('POST', '/v1/connect', body) as Json);
    });
    busy = false;
    _notify();
  }

  Future<void> reconnect() async {
    busy = true;
    _logAction(status.settingsPending ? 'переподключить, чтобы применить настройки' : 'переподключить');
    _notify();
    if (!await _vpnAllowed()) {
      busy = false;
      _notify();
      return;
    }
    await _act(() async => status = Status.fromJson(await backend.call('POST', '/v1/reconnect') as Json));
    busy = false;
    _notify();
  }

  /// [from] says where the user asked, when not in the window: "трей",
  /// "выход из приложения".
  Future<void> disconnect({String from = ''}) async {
    busy = true;
    _logAction(from.isEmpty ? 'отключить' : 'отключить ($from)');
    _notify();
    await _act(() async => status = Status.fromJson(await backend.call('POST', '/v1/disconnect') as Json));
    if (!status.active) speed.clear();
    busy = false;
    _notify();
  }

  /// Selects a node; while connected, switches the connection to it.
  Future<void> selectNode(String subscription, String fingerprint, [String name = '']) async {
    if (status.active) {
      await connect(subscription: subscription, fingerprint: fingerprint, name: name);
      return;
    }
    await _act(() async {
      selection = Selection.fromJson(
        await backend.call('PUT', '/v1/selection', {'subscription': subscription, 'fingerprint': fingerprint, 'name': name}) as Json,
      );
      _notify();
    });
  }

  /// Adds a subscription URL or a pasted list; returns the error, if any,
  /// for the dialog to show.
  Future<String?> addSubscription({required String source, String name = ''}) async {
    final isUrl = RegExp(r'^https?://\S+$').hasMatch(source.trim());
    try {
      final sub = Subscription.fromJson(
        await backend.call('POST', '/v1/subscriptions', {'name': name.trim(), if (isUrl) 'url': source.trim() else 'content': source}) as Json,
      );
      // Servers pasted without a name join the list pasted before.
      final old = subscriptions.where((s) => s.id == sub.id).firstOrNull;
      subscriptions = old == null ? [...subscriptions, sub] : [for (final s in subscriptions) s.id == sub.id ? sub : s];
      toast('Добавлено в «${sub.displayName}»: серверов: ${sub.nodes.length - (old?.nodes.length ?? 0)}', ToastKind.ok);
      if (sub.insecure) toast(insecureLinkWarning, ToastKind.err);
      _notify();
      // Nothing chosen yet: the first server that some core can run, so
      // the home page's button works straight away.
      if (selection.isEmpty) {
        final first = sub.nodes.where((n) => n.cores.isNotEmpty).firstOrNull;
        if (first != null) await selectNode(sub.id, first.fingerprint, first.name);
      }
      return null;
    } catch (e) {
      return humanError('$e');
    }
  }

  Future<void> refreshSubscription(String id) async {
    refreshing.add(id);
    _notify();
    await _act(() async {
      final sub = Subscription.fromJson(await backend.call('POST', '/v1/subscriptions/$id/refresh') as Json);
      subscriptions = [for (final s in subscriptions) s.id == id ? sub : s];
    });
    refreshing.remove(id);
    _notify();
  }

  Future<void> refreshAll() async {
    for (final s in subscriptions.where((s) => !s.isLocal).toList()) {
      await refreshSubscription(s.id);
    }
  }

  /// Tests the latency of the nodes of [subscription], or of all nodes.
  /// Results arrive as events while the test runs.
  ///
  /// The method is the service's own choice: a TCP or ICMP ping, and for a
  /// server the ping cannot time a request through its core. The setting
  /// that forced every server through a core has no control any more, so a
  /// copy left with it goes back to the automatic way.
  Future<void> testLatency([String? subscription]) async {
    testingLatency = true;
    _notify();
    if (setting('cores.latency_test', 'ping') == 'proxy') await updateSettings((s) => s['cores']['latency_test'] = 'ping');
    await _act(() => backend.call('POST', '/v1/latency', {'subscription': ?subscription}));
    testingLatency = false;
    _notify();
  }

  /// The whole link of subscription [id], which lists show without its
  /// token; null when the daemon cannot say.
  Future<String?> subscriptionUrl(String id) async {
    try {
      final url = ((await backend.call('GET', '/v1/subscriptions/$id/url')) as Json)['url'] as String?;
      return url == null || url.isEmpty ? null : url;
    } catch (e) {
      toast(humanError('$e'), ToastKind.err);
      return null;
    }
  }

  Future<void> renameSubscription(String id, String name) => _act(() async {
    final sub = Subscription.fromJson(await backend.call('PATCH', '/v1/subscriptions/$id', {'name': name}) as Json);
    subscriptions = [for (final s in subscriptions) s.id == id ? sub : s];
    _notify();
  });

  Future<void> removeSubscription(String id) => _act(() async {
    await backend.call('DELETE', '/v1/subscriptions/$id');
    subscriptions = subscriptions.where((s) => s.id != id).toList();
    _notify();
  });

  /// Changes settings: [edit] receives a copy to modify, which is saved as
  /// a whole. Returns the daemon's error, if any.
  Future<String?> updateSettings(void Function(Json s) edit) async {
    final draft = jsonDecode(jsonEncode(settings)) as Json;
    edit(draft);
    try {
      final before = settings;
      settings = await backend.call('PUT', '/v1/settings', draft) as Json;
      _syncAutostart();
      final changes = settingsChanges(before, settings);
      if (changes.isNotEmpty) _log(DateTime.now(), 'настройки', changes.join('; '), LogLevel.info);
      _notify();
      return null;
    } catch (e) {
      final msg = humanError('$e');
      toast(msg, ToastKind.err);
      _notify(); // redraw controls with the unchanged value
      return msg;
    }
  }

  Future<void> _reloadInfo() async {
    try {
      info = DaemonInfo.fromJson(await backend.call('GET', '/v1/info') as Json);
      _notify();
    } catch (_) {}
  }

  /// Moves back to the primary core now, when a backup is serving.
  Future<void> returnToPrimary() async {
    returning = true;
    _notify();
    try {
      status = Status.fromJson(await backend.call('POST', '/v1/cores/return') as Json);
    } catch (e) {
      final msg = '$e';
      final known = msg.contains('already on the primary') || msg.contains('not connected') || msg.contains('switching');
      toast(known ? humanError(msg) : 'Основное ядро пока не работает, остаёмся на резервном. ${humanError(msg)}', ToastKind.err);
      if (e is DaemonOffline) _lost(e);
    }
    returning = false;
    _notify();
  }

  /// Asks the daemon for the latest release of each core. Nothing is told:
  /// what is found is installed by [installCoreUpdates]. Returns the
  /// service's error for each core whose check failed, often only for want
  /// of a network, or for '' when the service could not be asked; the
  /// automatic check decides whether the journal hears of it.
  Future<Map<String, String>> checkCoreUpdates() async {
    checkingUpdates = true;
    _notify();
    try {
      final found = [for (final u in await backend.call('GET', '/v1/cores/updates') as List) CoreUpdate.fromJson((u as Map).cast())];
      // A core updated meanwhile keeps what its update said.
      if (updatingCore.isEmpty) coreUpdates = found;
      final line = coreCheckText(found);
      if (line != null) _log(DateTime.now(), 'ядра', line, LogLevel.info);
      return {for (final u in found.where((u) => u.error.isNotEmpty)) u.kind: u.error};
    } catch (e) {
      if (e is DaemonOffline) _lost(e);
      return {'': '$e'};
    } finally {
      checkingUpdates = false;
      _notify();
    }
  }

  /// What a check of the cores found, for the journal: the newer versions,
  /// or that every core is the latest, with their versions. null when no
  /// core could be checked: failures are told apart (coreCheckFailedText).
  static String? coreCheckText(List<CoreUpdate> found) {
    final checked = found.where((u) => u.error.isEmpty).toList();
    if (checked.isEmpty) return null;
    String v(CoreUpdate u, String version) => '${AppState.coreName(u.kind)} $version';
    final newer = checked.where((u) => u.available).toList();
    if (newer.isNotEmpty) {
      return 'найдены новые версии: ${newer.map((u) => '${v(u, u.latest)} (сейчас ${u.current})').join(', ')}';
    }
    return 'проверено: новых версий нет (${checked.map((u) => v(u, u.current)).join(', ')})';
  }

  CoreUpdate? updateOf(String kind) => coreUpdates.where((u) => u.kind == kind).firstOrNull;

  /// The cores with a newer version found and not installed yet.
  List<String> get coreUpdatesWaiting => [
    for (final u in coreUpdates)
      if (u.available && u.error.isEmpty && info.installed(u.kind)) u.kind,
  ];

  /// Installs every newer core found, one after another, without asking,
  /// with the VPN off or on: a VPN that is always on would otherwise never
  /// get one. A running core keeps its version until the service moves the
  /// connection to the new one, once it is quiet, without disconnecting.
  /// Not while a connection comes up, goes down or waits for the network.
  /// The service checks each download as it always does; the journal tells
  /// what was installed, and when it was applied.
  Future<void> installCoreUpdates() async {
    for (final kind in coreUpdatesWaiting) {
      if (!online || !_coreInstallTime || busy || updatingCore.isNotEmpty) return;
      await updateCore(kind);
    }
  }

  bool get _coreInstallTime => status.state == ConnState.idle || status.state == ConnState.failed || status.state == ConnState.connected;

  /// Installs the latest release of [kind]. Only a failure is told; the
  /// service's event puts the new version in the journal.
  Future<void> updateCore(String kind) async {
    updatingCore = kind;
    _notify();
    try {
      final r = await backend.call('POST', '/v1/cores/$kind/update') as Json;
      final v = r['version'] as String? ?? '';
      coreUpdates = [for (final u in coreUpdates) u.kind == kind ? CoreUpdate(kind: kind, current: v, latest: v) : u];
      await _reloadInfo();
    } catch (e) {
      final msg = 'Не удалось обновить ${AppState.coreName(kind)}: ${humanError('$e')}';
      _log(DateTime.now(), kind, msg, LogLevel.err);
      toast(msg, ToastKind.err);
      // Not tried again until the next check.
      coreUpdates = [for (final u in coreUpdates) u.kind == kind ? CoreUpdate(kind: kind, current: u.current, latest: u.latest, error: '$e') : u];
      if (e is DaemonOffline) _lost(e);
    }
    updatingCore = '';
    _notify();
  }

  /// The programs running now, for picking which bypass the tunnel.
  Future<List<RunningApp>> runningApps() async {
    final list = await backend.call('GET', '/v1/apps') as List;
    return [for (final a in list) RunningApp.fromJson((a as Map).cast())];
  }

  /// Checks whether DNS queries escape the tunnel; the result lands in
  /// [leakReport] or [leakError]. The engine runs the test, through the
  /// tunnel: on Android the app itself is outside the VPN.
  Future<void> runLeakTest() async {
    leakTesting = true;
    leakError = '';
    _notify();
    try {
      final runner = leakTestRunner ?? () async => await backend.call('POST', '/v1/leaktest') as Json;
      leakReport = LeakReport.fromJson(await runner());
    } catch (e) {
      leakReport = null;
      leakError = e is ApiError && e.status == 409
          ? 'Сначала подключитесь к VPN.'
          : e is DaemonOffline
          ? e.message
          : 'Не удалось связаться с сервисом проверки bash.ws через VPN. Проверьте, что сайты открываются, и попробуйте ещё раз.';
    }
    leakTesting = false;
    _notify();
  }

  bool get systemNotifications => prefs['notifications'] != false;
}
