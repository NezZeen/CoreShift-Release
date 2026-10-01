part of '../app_state.dart';

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
      'Режим: ${setting('tun', false) ? 'все приложения (TUN)' : 'только прокси'}; ядра: $cores; '
          'маршруты: ${setting('routing.mode', 'all') == 'selected' ? 'только выбранное' : 'всё через VPN'}; '
          'Россия напрямую: ${on('routing.russia_direct')}; IPv6: ${on('ipv6')}; fake-IP: ${on('dns.fake_ip')}',
      'Сейчас: ${AppState._stateText(status.state.name)}${status.core.isEmpty ? '' : ' через ${status.core}'}'
          '${selection.name.isEmpty ? '' : ', сервер «${selection.name}»'}',
      '',
    ];
  }

  /// Notes what the user did, so a journal copied from another computer
  /// says why the connection changed.
  void _logAction(String text) => _log(DateTime.now(), 'действие', text, LogLevel.info);

  /// Android asks the user once before the app may run a VPN.
  Future<bool> _vpnAllowed() async {
    if (backend is DemoBackend || !setting('tun', false)) return true;
    if (await platform.prepareVpn()) return true;
    toast('Android не разрешил VPN: без этого подключиться нельзя', ToastKind.err);
    return false;
  }

  Future<void> connect({String? subscription, String? fingerprint, String? name}) async {
    busy = true;
    _logAction(name != null && name.isNotEmpty ? 'подключить: $name' : 'подключить${selection.name.isEmpty ? '' : ': ${selection.name}'}');
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

  Future<void> disconnect() async {
    busy = true;
    _logAction('отключить');
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
  Future<void> testLatency([String? subscription]) async {
    testingLatency = true;
    _notify();
    await _act(() => backend.call('POST', '/v1/latency', {'subscription': ?subscription}));
    testingLatency = false;
    _notify();
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

  /// Asks the daemon for the latest release of each core.
  Future<void> checkCoreUpdates() async {
    checkingUpdates = true;
    _notify();
    await _act(() async {
      coreUpdates = [for (final u in await backend.call('GET', '/v1/cores/updates') as List) CoreUpdate.fromJson((u as Map).cast())];
      final failed = coreUpdates.where((u) => u.error.isNotEmpty).toList();
      if (failed.isNotEmpty && failed.length == coreUpdates.length) {
        toast(humanError(failed.first.error), ToastKind.err);
      } else if (!coreUpdates.any((u) => u.available)) {
        toast('Все ядра последних версий', ToastKind.ok);
      }
    });
    checkingUpdates = false;
    _notify();
  }

  CoreUpdate? updateOf(String kind) => coreUpdates.where((u) => u.kind == kind).firstOrNull;

  /// Installs the latest release of [kind].
  Future<void> updateCore(String kind) async {
    updatingCore = kind;
    _notify();
    await _act(() async {
      final r = await backend.call('POST', '/v1/cores/$kind/update') as Json;
      final v = r['version'] as String? ?? '';
      coreUpdates = [for (final u in coreUpdates) u.kind == kind ? CoreUpdate(kind: kind, current: v, latest: v) : u];
      await _reloadInfo();
      toast('${AppState.coreName(kind)} обновлён до $v${status.active ? '. Новая версия заработает после переподключения' : ''}', ToastKind.ok);
    });
    updatingCore = '';
    _notify();
  }

  /// The programs running now, for picking which bypass the tunnel.
  Future<List<RunningApp>> runningApps() async {
    final list = await backend.call('GET', '/v1/apps') as List;
    return [for (final a in list) RunningApp.fromJson((a as Map).cast())];
  }

  /// Checks whether DNS queries escape the tunnel; the result lands in
  /// [leakReport] or [leakError].
  Future<void> runLeakTest() async {
    leakTesting = true;
    leakError = '';
    _notify();
    try {
      final runner = leakTestRunner ?? (backend is DemoBackend ? DemoBackend.leakSample : platform.dnsLeakTest);
      leakReport = LeakReport.fromEntries(await runner());
    } catch (e) {
      leakReport = null;
      leakError = 'Не удалось связаться с сервисом проверки bash.ws. Проверьте, что интернет работает, и попробуйте ещё раз.';
    }
    leakTesting = false;
    _notify();
  }

  bool get systemNotifications => prefs['notifications'] != false;
}
