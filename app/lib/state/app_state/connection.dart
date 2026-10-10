part of '../app_state.dart';

/// Связь со службой: версия после обновления, подключение и переподключение, загрузка состояния и перечитывание.
extension AppStateConnection on AppState {
  /// Tells the user when this version differs from the one that ran last:
  /// an update, a return to an earlier version to compare, or a rebuild.
  void _noteUpdate() {
    if (!version.known) return;
    final prev = prefs['last_version'];
    if (prev is String && prev.isNotEmpty && prev != version.key) {
      final old = BuildVersion.parseKey(prev);
      // Builds between releases keep the version number: only a rebuild.
      final cmp = old.version == version.version ? 0 : version.compareTo(old);
      updateNotice = switch (cmp) {
        > 0 => 'CoreShift обновлён: ${old.label} → ${version.label}',
        < 0 => 'Установлена более ранняя версия CoreShift: ${old.label} → ${version.label}',
        _ => 'CoreShift пересобран: ${version.label}',
      };
      toast(updateNotice, ToastKind.ok);
      // After a self-update the app starts hidden in the tray.
      if (cmp > 0) _alerts.add(Alert('CoreShift обновлён', '${old.label} → ${version.label}'));
      _log(DateTime.now(), 'версия', updateNotice, LogLevel.info);
    }
    if (prev != version.key) setPref('last_version', version.key);
  }

  Future<void> _connectDaemon() async {
    _retry?.cancel();
    try {
      await _loadAll();
      _events?.cancel();
      // The daemon replays its recent events, the graph among them.
      speed.clear();
      _liveSince = DateTime.now().subtract(const Duration(seconds: 2));
      _events = backend.events().listen(_onEvent, onError: (Object e) => _lost(e), onDone: () => _lost('поток событий закрыт'));
      // A new stream opens shown: a hidden window says so again, once the
      // service has the stream.
      if (!_shown.value) Timer(const Duration(seconds: 2), _sendView);
      if (!online) {
        online = true;
        offlineReason = '';
      }
      daemonStarting = false;
      daemonStartRefused = false;
      _notify();
      if (!_coresChecked) unawaited(_autoCoreUpdates());
    } catch (e) {
      // The service runs only while the app does: the app starts it.
      final now = DateTime.now();
      // Without access to it, starting it would not help.
      if (e is DaemonOffline && e.accessDenied) {
        daemonStarting = false;
      } else if (e is DaemonOffline && daemonStarter != null && (_daemonStarted == null || now.difference(_daemonStarted!) > const Duration(seconds: 15))) {
        _daemonStarted = now;
        final ok = daemonStarter!();
        daemonStarting = ok;
        daemonStartRefused = !ok;
      }
      _lost(e);
    }
  }

  void _lost(Object reason) {
    if (_disposed) return;
    _events?.cancel();
    _events = null;
    if (online && status.active) {
      _alerts.add(const Alert('Служба CoreShift не отвечает', 'VPN может не работать. Откройте CoreShift, чтобы узнать подробности.'));
    }
    online = false;
    // A started service comes up within seconds; after that it is stuck.
    final sinceStart = _daemonStarted == null ? null : DateTime.now().difference(_daemonStarted!);
    if (daemonStarting && sinceStart != null && sinceStart > const Duration(seconds: 20)) daemonStarting = false;
    offlineReason = daemonStarting
        ? 'Запускаем службу CoreShift'
        : reason is DaemonOffline || reason is ApiError
        ? '$reason'
        : 'Служба CoreShift не отвечает';
    _notify();
    _retry?.cancel();
    // While it starts, look often, so the window is ready the moment it is.
    _retry = Timer(daemonStarting ? const Duration(milliseconds: 300) : const Duration(seconds: 2), _connectDaemon);
  }

  Future<void> _loadAll() async {
    final results = await Future.wait([
      backend.call('GET', '/v1/info'),
      backend.call('GET', '/v1/status'),
      backend.call('GET', '/v1/settings'),
      backend.call('GET', '/v1/subscriptions'),
      backend.call('GET', '/v1/selection'),
    ]);
    info = DaemonInfo.fromJson(results[0] as Json);
    status = Status.fromJson(results[1] as Json);
    settings = results[2] as Json;
    _syncAutostart();
    subscriptions = _subs(results[3]);
    selection = Selection.fromJson(results[4] as Json);
    loaded = true;
    checkSubscriptions();
    // Optional, so it does not hold up the rest.
    unawaited(_loadAppUpdate().then((_) => _notify()));
  }

  List<Subscription> _subs(dynamic j) {
    final subs = (j as List).map((s) => Subscription.fromJson((s as Map).cast())).toList();
    for (final s in subs) {
      for (final n in s.nodes) {
        if (n.latency != null) latency['${s.id}/${n.fingerprint}'] = n.latency!;
      }
    }
    return subs;
  }

  Latency? latencyOf(String subscription, String fingerprint) => latency['$subscription/$fingerprint'];

  Future<void> _reloadStatus() async {
    try {
      status = Status.fromJson(await backend.call('GET', '/v1/status') as Json);
      _notify();
    } catch (_) {}
  }

  void _statusSoon() {
    _statusDebounce?.cancel();
    _statusDebounce = Timer(const Duration(milliseconds: 80), _reloadStatus);
  }

  Future<void> _reloadStore(String what) async {
    try {
      if (what == 'settings') {
        settings = await backend.call('GET', '/v1/settings') as Json;
        _syncAutostart();
      } else if (what == 'selection') {
        selection = Selection.fromJson(await backend.call('GET', '/v1/selection') as Json);
      } else {
        final r = await Future.wait([backend.call('GET', '/v1/subscriptions'), backend.call('GET', '/v1/selection')]);
        subscriptions = _subs(r[0]);
        selection = Selection.fromJson(r[1] as Json);
        checkSubscriptions();
      }
      _notify();
    } catch (_) {}
  }
}
