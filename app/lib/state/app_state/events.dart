part of '../app_state.dart';

/// Что служба присылает событиями (Event.kind) и что из этого идёт в журнал, в тосты и уведомления.
extension AppStateEvents on AppState {
  void _onEvent(Event e) {
    final live = !e.time.isBefore(_liveSince);
    switch (e.kind) {
      case 'state':
        _stateChangedAt = e.time;
        if (e.state == 'connecting') {
          _healthFailing.clear();
          _healthStreak.clear();
          _directBlocked = false;
        }
        if (e.state != 'connected') _serverProblem = null;
        // A connection that ends is summed up, before the line that says
        // what comes next.
        // A replayed end is summed up once, whatever the traffic replayed.
        final ended = _sessionEnd(e);
        if (ended != null && !logs.any((l) => l.time.isAtSameMomentAs(e.time) && l.message.startsWith('сессия:'))) {
          _log(e.time, 'служба', ended, LogLevel.info);
        }
        // Without a network the "network" event beside it says what goes on.
        if (e.state != 'no-network') {
          _log(
            e.time,
            'служба',
            _sessionStateText(e) + (e.error.isNotEmpty ? ': ${journalCodedError(e.code, e.args, e.error)}' : ''),
            e.state == 'failed' ? LogLevel.err : (e.state == 'connected' ? LogLevel.ok : LogLevel.info),
          );
        }
        _failedWith = e.state == 'failed' ? e.error : '';
        if (e.state == 'connecting') {
          latencies.clear();
          swaps = 0;
          speed.clear();
          sessionUp = sessionDown = 0;
          _statsBaseUp = _statsBaseDown = 0;
        }
        if (e.state == 'idle' || e.state == 'failed') speed.clear();
        if (e.state == 'idle') _statsSoon();
        // A core update found while a connection came up or went down
        // waits for it to settle.
        if ((e.state == 'idle' || e.state == 'connected') && live && coreUpdatesWaiting.isNotEmpty) Timer(const Duration(seconds: 2), installCoreUpdates);
        if (e.state == 'failed' && live) {
          final why = humanCodedError(e.code, e.args, e.error);
          toast(why, ToastKind.err);
          _alerts.add(Alert('VPN отключился', why));
        }
        _statusSoon();
      case 'core-state':
        // Stopping reports no core: the whole chain is stopped.
        if (!e.probe) _log(e.time, e.core.isEmpty ? 'ядра' : e.core, AppState._coreStateText(e.reason), LogLevel.info);
        _statusSoon();
      case 'swap':
        swaps++;
        _log(e.time, 'автосвап', '${AppState.coreName(e.from)} → ${AppState.coreName(e.core)} (${AppState._reasonText(e.reason)})', LogLevel.swap);
        if (live) {
          final back = e.reason == 'return-to-primary';
          toast(
            back
                ? 'Снова работает основное ядро: ${AppState.coreName(e.core)}'
                : 'Ядро переключено с ${AppState.coreName(e.from)} на ${AppState.coreName(e.core)}',
            ToastKind.swap,
          );
          _alerts.add(
            back
                ? Alert('Основное ядро снова работает', AppState.coreName(e.core))
                : Alert(
                    'CoreShift сменил ядро',
                    '${AppState.coreName(e.from)}: ${AppState._reasonText(e.reason)}. Теперь работает ${AppState.coreName(e.core)}, VPN не прерывался.',
                  ),
          );
        }
        _statusSoon();
      case 'no-better':
        // The engine now looks whether the server or the network is at
        // fault, and its "server" event says which.
        _log(e.time, e.core, 'ни одно ядро не проходит проверку связи, проверяю сервер и сеть напрямую', LogLevel.warn);
      case 'server':
        _onServerEvent(e, live);
      case 'direct':
        _onDirectEvent(e);
      case 'failover':
        if (e.error.isNotEmpty) {
          _log(e.time, 'автопереход', 'ни один другой сервер подписки не отвечает', LogLevel.err);
          if (live) toast('Ни один другой сервер подписки не отвечает', ToastKind.err);
        } else {
          _log(e.time, 'автопереход', 'сервер «${e.from}» не отвечает, подключаюсь к «${e.line}»', LogLevel.swap);
          if (live) {
            toast('Сервер «${e.from}» не отвечал: подключено «${e.line}»', ToastKind.swap);
            _alerts.add(Alert('CoreShift сменил сервер', '«${e.from}» не отвечал. Теперь «${e.line}».'));
          }
        }
        _statusSoon();
      case 'core-failed':
        _log(e.time, e.core, 'отключено (${AppState._reasonText(e.reason)}): ${e.error}', LogLevel.err);
        _statusSoon();
      case 'core-restart':
        _log(e.time, e.core, 'завис и перезапущен: ${e.error}', LogLevel.swap);
        _statusSoon();
      case 'health':
        if (e.probe) break;
        // While it fails the check repeats every few seconds: the log says
        // when it starts failing and when it works again.
        if (e.error.isNotEmpty) {
          if (_healthFailing.add(e.core)) _log(e.time, e.core, 'проверка связи не прошла: ${e.error}', LogLevel.warn);
          final streak = _healthStreak[e.core] = (_healthStreak[e.core] ?? 0) + 1;
          if (streak == 2) _notify();
        } else {
          if (_healthFailing.remove(e.core)) _log(e.time, e.core, 'проверка связи снова проходит', LogLevel.ok);
          _healthStreak.remove(e.core);
          latencies.add(e.latencyMs);
          if (latencies.length > 60) latencies.removeAt(0);
          _notify();
        }
      case 'log':
        _log(e.time, e.source, e.lineText, _outputLevel(e.line), output: true);
      case 'app-update':
        _onAppUpdate(e, live);
      case 'tun' when e.reason == 'retry':
        // The failed attempt's own FATAL line is in the journal just above:
        // this one says the next attempt follows.
        _log(e.time, 'TUN', 'интерфейс не поднялся: Windows ещё убирает прежний адаптер; повтор через ${e.line}. Причина: ${e.error}', LogLevel.warn);
      case 'tun' when e.reason == 'updated':
        // The desktop's TUN layer is a sing-box of its own: after the core,
        // it moves to an updated sing-box too, restarting the interface.
        _log(e.time, 'TUN', 'слой TUN перешёл на sing-box ${e.line}: интерфейс перезапущен, VPN не отключался', LogLevel.ok);
      case 'dns' when e.reason == 'network-changed':
        // The reconnect that follows says why it happens.
        _log(e.time, 'сеть', e.line.isNotEmpty ? e.lineText : AppState._layerText(e.kind, e.reason), LogLevel.swap);
        if (live) toast('Сеть сменилась — CoreShift переподключается');
      case 'tun':
      case 'dns':
        _log(
          e.time,
          e.kind.toUpperCase(),
          e.error.isNotEmpty ? e.errorText : AppState._layerText(e.kind, e.reason),
          e.error.isNotEmpty ? LogLevel.warn : LogLevel.info,
        );
      case 'proxy':
        _log(e.time, 'прокси', e.error.isNotEmpty ? e.errorText : e.lineText, e.error.isNotEmpty ? LogLevel.warn : LogLevel.info);
      case 'action':
        // Why the connection changes, when not by the window's buttons:
        // Android's tile and notification, "Автозапуск", the service itself.
        _log(e.time, e.source.isEmpty ? 'действие' : e.source, e.lineText, LogLevel.info);
      case 'error':
        // Services before 0.7.2 sent a failure twice: in the state and here.
        if (e.error == _failedWith) break;
        _log(e.time, 'служба', journalError(e.error), LogLevel.err);
        if (live) toast(humanError(e.error), ToastKind.err);
      case 'network':
        _onNetworkEvent(e, live);
      case 'netinfo':
        // Which network the connection runs over, and a move to another.
        _log(e.time, 'сеть', e.lineText, e.reason == 'changed' ? LogLevel.swap : LogLevel.info);
      case 'traffic':
        // Hidden, the samples come every few seconds: the graph, a sample a
        // second, keeps only the latest of them rather than squeezing
        // minutes into its two.
        if (!_shown.value && _sampleHidden && speed.isNotEmpty) speed.removeLast();
        _sampleHidden = !_shown.value;
        speed.add((e.upRate, e.downRate));
        if (speed.length > AppState.speedKeep) speed.removeRange(0, speed.length - AppState.speedKeep);
        sessionUp = e.up;
        sessionDown = e.down;
        if (_disposed) break;
        _speedNow.value++;
        if (_shown.value) {
          _traffic.value++;
        } else {
          _trafficMissed = true;
        }
      case 'cores' when e.reason == 'applied':
        _log(e.time, e.core, 'обновление до ${e.line} применено без отключения VPN', LogLevel.ok);
      case 'cores':
        _log(e.time, e.core, 'обновлено до ${e.line}', LogLevel.ok);
        _reloadInfo();
      case 'store':
        if (e.error.isNotEmpty) _log(e.time, 'подписки', 'обновление не удалось: ${e.error}', LogLevel.warn);
        _reloadStore(e.reason);
      case 'options':
        _statusSoon();
      case 'latency':
        if (e.reason == 'started') {
          testingLatency = true;
          latencyDone = 0;
          latencyTotal = subscriptions.where((s) => e.subscription.isEmpty || s.id == e.subscription).fold(0, (n, s) => n + s.nodes.length);
        } else if (e.reason == 'finished') {
          testingLatency = false;
          latencyDone = latencyTotal;
        } else if (e.fingerprint.isNotEmpty) {
          latency['${e.subscription}/${e.fingerprint}'] = Latency(ms: e.latencyMs, error: e.error, core: e.core, method: e.method);
          if (latencyDone < latencyTotal) latencyDone++;
        }
        _notify();
      case 'speedtest':
        _onSpeedEvent(e);
      case 'checkup':
        _onCheckupEvent(e);
      case 'rules':
        if (e.error.isNotEmpty) {
          _log(e.time, 'правила', e.errorText, LogLevel.warn);
          // Only the direct sets of the Russian preset failing changes much
          // at once. A set that was not updated ("kept"), damaged on disk or
          // not had from the chosen source (the copy built into CoreShift
          // takes its place) still works; without geosite-google the
          // built-in list keeps Google in the tunnel.
          const quiet = {'kept', 'damaged', 'fallback', 'pending'};
          if (live && e.line == 'skipped') {
            toast('Правило «${e.reason}» пока не действует: база не загрузилась. Подробности в журнале.', ToastKind.err);
          } else if (live && !quiet.contains(e.line) && (e.reason == 'geosite-category-ru' || e.reason == 'geoip-ru')) {
            toast('Базы российских сайтов не загрузились — они пойдут через туннель. Подробности в журнале.', ToastKind.err);
          }
        } else {
          final what = switch (e.line) {
            'updated' => 'обновлена',
            'builtin' => 'взята встроенная копия',
            _ => 'загружена',
          };
          _log(e.time, 'правила', '${e.reason}: $what', LogLevel.info);
        }
    }
  }

  /// The device lost its network or got it back (engine/internal/service/
  /// netwatch.go): the journal says so in words, a toast too as it happens.
  void _onNetworkEvent(Event e, bool live) {
    final (text, level, note) = switch (e.reason) {
      'waiting' => ('сети нет: подключусь, как только она появится', LogLevel.warn, 'Нет сети — CoreShift подключится, когда она появится'),
      'lost' => ('сеть пропала: VPN ждёт её, ядра и сервер не меняются', LogLevel.warn, 'Пропала сеть — VPN подождёт её'),
      'back' => ('сеть вернулась', LogLevel.ok, 'Сеть вернулась'),
      'reconnect' => ('сеть вернулась, но связь через сервер не восстановилась: переподключаюсь', LogLevel.swap, ''),
      _ => (e.line.isNotEmpty ? e.lineText : e.reason, LogLevel.info, ''),
    };
    _log(e.time, 'сеть', text, level);
    if (live && note.isNotEmpty) toast(note, level == LogLevel.ok ? ToastKind.ok : ToastKind.info);
    _statusSoon();
  }

  /// What the engine found when nothing got through: who is at fault
  /// (reason, see [Status.problem]), or that the server answers again.
  void _onServerEvent(Event e, bool live) {
    if (e.reason == 'ok') {
      _serverProblem = null;
      _log(e.time, 'сервер', 'сервер «${e.from}» снова отвечает', LogLevel.ok);
      _notify();
      return;
    }
    _serverProblem = e.reason;
    final (title, body) = AppState.serverProblemText(e.reason, e.from);
    _log(e.time, 'сервер', '$title${e.line.isEmpty ? '' : ' (${e.lineText})'}', LogLevel.err);
    if (live) {
      toast(title, ToastKind.err);
      // Out of the window's sight: the desktop's tray, the phone's
      // notification, which opens the app where the banner offers the way out.
      _alerts.add(Alert(title, body));
      platform.notify(title, body);
    }
    _notify();
  }
}
