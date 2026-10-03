import 'dart:async';
import 'dart:convert';
import 'dart:math';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../api/backend.dart';
import '../api/demo_backend.dart';
import '../api/models.dart';
import '../platform/platform.dart' as platform;
import 'errors.dart';
import 'import_link.dart';
import 'leak.dart';
import '../version.dart';

export 'import_link.dart' show ImportLink;

part 'app_state/models.dart';
part 'app_state/actions.dart';
part 'app_state/settings_diff.dart';
part 'app_state/servers.dart';
part 'app_state/traffic.dart';
part 'app_state/speed_test.dart';
part 'app_state/imports.dart';
part 'app_state/sub_alerts.dart';

/// Everything the UI shows, kept in sync with the daemon through its event
/// stream. Widgets listen to it and call its actions.
class AppState extends ChangeNotifier {
  final Backend backend;

  /// UI preferences kept per user (theme, notifications); [savePrefs]
  /// stores them. Tests leave it out and so never touch the real file.
  final Json prefs;
  final Future<void> Function(Json prefs)? savePrefs;

  /// Runs the DNS leak test; by default the real one, or a sample on demo
  /// data.
  final Future<List<Json>> Function()? leakTestRunner;

  /// Opens links; by default in the browser, not at all on demo data.
  final Future<bool> Function(String url)? linkOpener;

  /// Cores whose health checks fail right now, to log only the change.
  final _healthFailing = <String>{};

  /// How many checks in a row each core has failed; the first failure is
  /// often a blip, so the page speaks up from the second.
  final _healthStreak = <String, int>{};

  /// The connection is up, but the server does not answer: the check through
  /// the core in use keeps failing (when others did no better, the engine
  /// leaves the connection as it is, see the "no-better" event).
  bool get serverUnresponsive => status.state == ConnState.connected && (_healthStreak[status.core] ?? 0) >= 2;

  /// Feeds an event as if the service had sent it, for tests.
  @visibleForTesting
  void injectEvent(Event e) => _onEvent(e);

  AppState(
    this.backend, {
    Json? prefs,
    this.savePrefs,
    this.leakTestRunner,
    this.linkOpener,
    this.daemonStarter,
    this.autostartSetter,
    this.version = BuildVersion.app,
  }) : prefs = prefs ?? {};

  /// Starts a stopped daemon, where the app may (the Windows service);
  /// returns whether it runs or is starting.
  final bool Function()? daemonStarter;
  DateTime? _daemonStarted;

  /// The app started the service and waits for it to come up, which takes
  /// a second or two: meanwhile the app says so rather than that it is off.
  bool daemonStarting = false;

  /// Windows did not let the app start the service, as with services
  /// installed before 0.4: it takes administrator rights.
  bool daemonStartRefused = false;

  /// Makes the system start the app at sign-in, or stops it; follows the
  /// "Автозапуск" setting. Returns whether that worked.
  final bool Function(bool on)? autostartSetter;
  bool? _autostart;

  void _syncAutostart() {
    final on = settings['auto_connect'] == true;
    if (autostartSetter == null || on == _autostart) return;
    if (autostartSetter!(on)) _autostart = on;
  }

  /// This app's version; tests pass their own.
  final BuildVersion version;

  /// What changed since the app last ran, e.g. "0.1.0 → 0.2.0";
  /// empty when nothing did.
  String updateNotice = '';

  /// The service runs different code than the app: one of them was
  /// updated without the other.
  bool get versionMismatch => version.known && info.version.isNotEmpty && !version.same(info.buildVersion);

  bool online = false;
  bool loaded = false;
  String offlineReason = '';

  Status status = const Status();
  DaemonInfo info = const DaemonInfo();
  Json settings = {};
  List<Subscription> subscriptions = [];
  Selection selection = const Selection();

  final List<LogLine> logs = [];
  final List<int> latencies = []; // recent health checks, oldest first
  int swaps = 0;

  /// Bytes per second through the node, (up, down) per second, oldest first.
  final List<(int, int)> speed = [];
  int sessionUp = 0;
  int sessionDown = 0;
  static const speedKeep = 120;

  /// The latest release of each core, once checked.
  List<CoreUpdate> coreUpdates = [];

  /// Updates of CoreShift itself; off with a service that cannot update.
  AppUpdateInfo appUpdate = const AppUpdateInfo();
  bool checkingUpdates = false;
  String updatingCore = '';

  /// Set while moving back to the primary core.
  bool returning = false;

  LeakReport? leakReport;
  String leakError = '';
  bool leakTesting = false;

  /// The last speed test, or the one running (speed_test.dart).
  SpeedTestState speedTest = const SpeedTestState();

  /// The service is older than the speed test.
  bool speedUnsupported = false;

  /// A subscription to add that came from outside the add dialog, waiting
  /// for the user to agree (imports.dart).
  ImportLink? pendingImport;
  ImportFrom importFrom = ImportFrom.link;

  /// Looks at the subscriptions' terms now and then (sub_alerts.dart).
  Timer? _subTimer;

  final _alerts = StreamController<Alert>.broadcast();

  /// Core swaps and dropped connections, for system notifications.
  Stream<Alert> get alerts => _alerts.stream;

  /// Events older than this were replayed on connecting to the daemon:
  /// they fill the log but are not news worth a toast.
  DateTime _liveSince = DateTime.now();

  final List<Toast> toasts = [];
  int _toastSeq = 0;

  /// Last latency test per node, by subscription + '/' + fingerprint.
  final Map<String, Latency> latency = {};

  /// Set while the daemon tests node latency.
  bool testingLatency = false;

  /// How far the running test is: nodes answered of nodes asked.
  int latencyDone = 0;
  int latencyTotal = 0;

  /// Whether the servers page has already tested latency on its own this
  /// session, so opening it again does not start another test.
  bool latencyAutoTested = false;

  /// Set while a connect or disconnect request is in flight.
  bool busy = false;
  final Set<String> refreshing = {};

  bool _disposed = false;
  StreamSubscription<Event>? _events;
  Timer? _retry;
  Timer? _statusDebounce;

  // ---------------------------------------------------------------- sync

  void start() {
    _noteUpdate();
    _connectDaemon();
    // A subscription runs out while the app runs, not only on a refresh.
    _subTimer = Timer.periodic(const Duration(hours: 1), (_) => checkSubscriptions());
    // The cores' new versions are looked for by themselves, once a day.
    _coreTimer = Timer.periodic(const Duration(hours: 24), (_) => _autoCoreUpdates());
  }

  Timer? _coreTimer;
  bool _coresChecked = false;

  /// Looks for newer cores and installs them, without a word unless it
  /// fails. Android updates its cores with the app.
  Future<void> _autoCoreUpdates() async {
    if (platform.isAndroid || !online || checkingUpdates || updatingCore.isNotEmpty) return;
    _coresChecked = true;
    await checkCoreUpdates();
    await installCoreUpdates();
  }

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
      if (e is DaemonOffline && daemonStarter != null && (_daemonStarted == null || now.difference(_daemonStarted!) > const Duration(seconds: 15))) {
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

  /// Services before 0.3.0 have no self-update: it stays off for them.
  Future<void> _loadAppUpdate() async {
    try {
      appUpdate = AppUpdateInfo.fromJson(await backend.call('GET', '/v1/app-update') as Json);
    } catch (_) {
      appUpdate = const AppUpdateInfo();
    }
  }

  Future<bool> checkAppUpdate() => _act(() async {
    appUpdate = AppUpdateInfo.fromJson(await backend.call('POST', '/v1/app-update/check') as Json);
    _notify();
  });

  /// Installs the downloaded update now; a connection comes back after it.
  /// On Android the system's installer asks the user first, once Android
  /// lets CoreShift install apps at all.
  Future<bool> installAppUpdate() async {
    if (!await platform.canInstallUpdates()) {
      _installWhenAllowed = true;
      await platform.allowInstallUpdates();
      toast('Разрешите CoreShift устанавливать приложения и вернитесь: обновление продолжится.');
      return false;
    }
    return _act(() async {
      appUpdate = AppUpdateInfo.fromJson(await backend.call('POST', '/v1/app-update/install') as Json);
      _notify();
    });
  }

  /// The user went to allow installing apps for an update.
  bool _installWhenAllowed = false;

  /// Back in the app: the update goes on if the user allowed installing.
  Future<void> resumed() async {
    if (!_installWhenAllowed) return;
    _installWhenAllowed = false;
    if (appUpdate.state == 'ready' && await platform.canInstallUpdates()) await installAppUpdate();
  }

  /// The downloaded update is offered in a window once per version while
  /// the app runs: "Позже" means until the next start.
  String _updateOffered = '';

  /// On the desktop an update that installs by itself right away (automatic
  /// updates on, VPN off) is not offered: the installer is already starting.
  bool get offerUpdate =>
      appUpdate.state == 'ready' && appUpdate.label != _updateOffered && (platform.isAndroid || appUpdate.waiting || !setting('app_update.auto', true));

  void updateOffered() => _updateOffered = appUpdate.label;

  /// Opens a link the panel sent (its support chat, the subscription's
  /// page). Only web, Telegram and mail links: another scheme could start
  /// a program. The link is not shown, it may carry the subscription's token.
  Future<void> openLink(String url) async {
    final u = Uri.tryParse(url.trim());
    final ok = u != null && (u.scheme == 'tg' || u.scheme == 'mailto' || ((u.scheme == 'https' || u.scheme == 'http') && u.host.isNotEmpty));
    if (!ok) {
      toast('Панель прислала ссылку, которую нельзя открыть', ToastKind.err);
      return;
    }
    if (linkOpener == null && backend is DemoBackend) {
      toast('В демо-режиме ссылки не открываются');
      return;
    }
    try {
      if (await (linkOpener ?? platform.openUrl)(u.toString())) return;
    } catch (_) {}
    toast('Не удалось открыть ссылку: нет приложения, которое её откроет', ToastKind.err);
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

  void _onEvent(Event e) {
    final live = !e.time.isBefore(_liveSince);
    switch (e.kind) {
      case 'state':
        if (e.state == 'connecting') {
          _healthFailing.clear();
          _healthStreak.clear();
        }
        _log(
          e.time,
          'служба',
          _stateText(e.state) + (e.error.isNotEmpty ? ': ${e.error}' : ''),
          e.state == 'failed' ? LogLevel.err : (e.state == 'connected' ? LogLevel.ok : LogLevel.info),
        );
        if (e.state == 'connecting') {
          latencies.clear();
          swaps = 0;
          speed.clear();
          sessionUp = sessionDown = 0;
          _statsBaseUp = _statsBaseDown = 0;
        }
        if (e.state == 'idle' || e.state == 'failed') speed.clear();
        if (e.state == 'idle') _statsSoon();
        // A core update found while connected waits for the VPN to be off.
        if (e.state == 'idle' && live && coreUpdatesWaiting.isNotEmpty) Timer(const Duration(seconds: 2), installCoreUpdates);
        if (e.state == 'failed' && live) _alerts.add(Alert('VPN отключился', humanError(e.error)));
        _statusSoon();
      case 'core-state':
        // Stopping reports no core: the whole chain is stopped.
        if (!e.probe) _log(e.time, e.core.isEmpty ? 'ядра' : e.core, _coreStateText(e.reason), LogLevel.info);
        _statusSoon();
      case 'swap':
        swaps++;
        _log(e.time, 'автосвап', '${coreName(e.from)} → ${coreName(e.core)} (${_reasonText(e.reason)})', LogLevel.swap);
        if (live) {
          final back = e.reason == 'return-to-primary';
          toast(back ? 'Снова работает основное ядро: ${coreName(e.core)}' : 'Ядро переключено с ${coreName(e.from)} на ${coreName(e.core)}', ToastKind.swap);
          _alerts.add(
            back
                ? Alert('Основное ядро снова работает', coreName(e.core))
                : Alert('CoreShift сменил ядро', '${coreName(e.from)}: ${_reasonText(e.reason)}. Теперь работает ${coreName(e.core)}, VPN не прерывался.'),
          );
        }
        _statusSoon();
      case 'no-better':
        _log(e.time, e.core, 'ни одно ядро не проходит проверку связи, подключение остаётся на ${coreName(e.core)}', LogLevel.warn);
        // In an automatic selection the next server is tried, and its event speaks.
        if (live && !autoSwitching) {
          toast('Проверка связи не проходит ни через одно ядро. VPN остаётся включённым: возможно, дело в сети', ToastKind.info);
        }
      case 'failover':
        if (e.error.isNotEmpty) {
          _log(e.time, 'автопереход', 'ни один другой сервер подписки не отвечает', LogLevel.err);
          if (live) toast('Ни один сервер подписки не отвечает: возможно, дело в сети', ToastKind.err);
        } else {
          _log(e.time, 'автопереход', 'сервер «${e.from}» не отвечает, подключаюсь к «${e.line}»', LogLevel.swap);
          if (live) {
            toast('Сервер «${e.from}» не отвечал: подключено «${e.line}»', ToastKind.swap);
            _alerts.add(Alert('CoreShift сменил сервер', '«${e.from}» не отвечал. Теперь «${e.line}».'));
          }
        }
        _statusSoon();
      case 'core-failed':
        _log(e.time, e.core, 'отключено (${_reasonText(e.reason)}): ${e.error}', LogLevel.err);
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
        _log(e.time, e.source, e.line, LogLevel.info, quiet: true);
      case 'app-update':
        _onAppUpdate(e, live);
      case 'tun':
      case 'dns':
        _log(e.time, e.kind.toUpperCase(), e.error.isNotEmpty ? e.error : _layerText(e.kind, e.reason), e.error.isNotEmpty ? LogLevel.warn : LogLevel.info);
        if (live && e.reason == 'network-changed') toast('Сеть сменилась — CoreShift переподключается');
      case 'error':
        _log(e.time, 'служба', e.error, LogLevel.err);
        if (live) toast(humanError(e.error), ToastKind.err);
      case 'traffic':
        speed.add((e.upRate, e.downRate));
        if (speed.length > speedKeep) speed.removeRange(0, speed.length - speedKeep);
        sessionUp = e.up;
        sessionDown = e.down;
        _notify();
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
      case 'rules':
        if (e.error.isNotEmpty) {
          _log(e.time, 'правила', e.error, LogLevel.warn);
          if (live) toast('Базы российских сайтов не загрузились — они пойдут через туннель. Подробности в журнале.', ToastKind.err);
        } else {
          _log(e.time, 'правила', '${e.reason}: ${e.line == 'updated' ? 'обновлена' : 'загружена'}', LogLevel.info);
        }
    }
  }

  void _log(DateTime t, String source, String msg, LogLevel level, {bool quiet = false}) {
    logs.add(LogLine(t, source, msg, level));
    if (logs.length > 2000) logs.removeRange(0, logs.length - 2000);
    if (!quiet || logs.length % 20 == 0) _notify();
  }

  static String _stateText(String s) => switch (s) {
    'connecting' => 'подключение…',
    'connected' => 'подключено',
    'disconnecting' => 'отключение…',
    'failed' => 'ошибка',
    _ => 'отключено',
  };

  static String _coreStateText(String s) => switch (s) {
    'starting' => 'запуск',
    'connected' => 'работает',
    'connecting' => 'проверка связи',
    'swapping' => 'переключение',
    'failed' => 'все отказали',
    'idle' => 'остановлены',
    _ => s,
  };

  static String reasonText(String r) => _reasonText(r);

  static String _reasonText(String r) => switch (r) {
    'start-failed' => 'не запустилось',
    'exited' => 'процесс завершился',
    'hung' => 'завис',
    'health-check' || 'health' => 'нет связи',
    'latency' => 'высокая задержка',
    'return-to-primary' || 'return' => 'возврат к основному',
    _ => r,
  };

  static String coreName(String k) => switch (k) {
    'xray' => 'Xray-core',
    _ => k,
  };

  Future<void> _onAppUpdate(Event e, bool live) async {
    if (e.error.isNotEmpty) _log(e.time, 'обновление', e.error, LogLevel.warn);
    if (e.reason == 'installed') return; // the new app says so itself
    await _loadAppUpdate();
    if (live && e.reason == 'ready') {
      final label = appUpdate.label;
      // The window offering it says the rest (see Shell).
      if (appUpdate.waiting) {
        _log(e.time, 'обновление', 'скачана версия $label, установится после отключения VPN', LogLevel.info);
      } else if (platform.isAndroid) {
        _log(e.time, 'обновление', 'скачана версия $label', LogLevel.info);
      } else {
        _log(e.time, 'обновление', 'скачана версия $label, устанавливается', LogLevel.info);
      }
    }
    _notify();
  }

  static String _layerText(String kind, String reason) => switch ((kind, reason)) {
    ('tun', 'up') => 'интерфейс поднят',
    ('tun', 'down') => 'интерфейс остановлен',
    ('dns', 'applied') => 'системный DNS направлен в туннель',
    ('dns', 'reverted') => 'системный DNS восстановлен',
    ('dns', 'network-changed') => 'сеть сменилась, прежний DNS недоступен — переподключение',
    _ => reason,
  };

  // ---------------------------------------------------------------- toasts

  void toast(String message, [ToastKind kind = ToastKind.info]) {
    // A failed connect reports its error twice: as the response and as an
    // event of the daemon.
    if (toasts.any((t) => t.message == message)) return;
    final t = Toast(++_toastSeq, message, kind);
    toasts.add(t);
    _notify();
    Timer(const Duration(seconds: 5), () {
      toasts.remove(t);
      _notify();
    });
  }

  void dismissToast(Toast t) {
    toasts.remove(t);
    _notify();
  }

  /// Runs an action, turning failures into an error toast. Returns whether
  /// it succeeded.
  Future<bool> _act(Future<void> Function() f) async {
    try {
      await f();
      return true;
    } catch (e) {
      toast(humanError('$e'), ToastKind.err);
      if (e is DaemonOffline) _lost(e);
      return false;
    }
  }

  // ---------------------------------------------------------------- address

  /// The address sites see, once looked up; null before, or when the
  /// daemon is older than the lookup.
  IpInfo? publicIp;
  bool ipLoading = false;
  String ipError = '';
  bool ipUnsupported = false;
  String _ipKey = '';
  Timer? _ipTimer;

  /// Looks the address up again when the connection changed since the last
  /// lookup: on connecting, disconnecting and moving to another server.
  /// Called by the page that shows it, so no other page causes lookups.
  void watchIp() {
    final st = status;
    if (!online || ipUnsupported || (st.state != ConnState.connected && st.state != ConnState.idle)) return;
    final key = '${st.state.name}|${st.node}';
    if (key == _ipKey) return;
    _ipKey = key;
    // A moment after connecting, for the routes to settle.
    _ipTimer?.cancel();
    _ipTimer = Timer(Duration(milliseconds: st.state == ConnState.connected ? 800 : 0), refreshIp);
  }

  Future<void> refreshIp() async {
    if (ipLoading || _disposed) return;
    ipLoading = true;
    _notify();
    try {
      publicIp = IpInfo.fromJson(await backend.call('GET', '/v1/ip') as Json);
      ipError = '';
    } on ApiError catch (e) {
      if (e.status == 404 || e.status == 405) ipUnsupported = true;
      ipError = 'Не удалось узнать адрес';
    } catch (_) {
      ipError = 'Не удалось узнать адрес';
    }
    ipLoading = false;
    _notify();
  }

  // ---------------------------------------------------------------- traffic

  /// Traffic per day, oldest first, once loaded (see traffic.dart).
  List<TrafficDay> stats = [];
  bool statsLoaded = false;
  bool statsUnsupported = false;
  String _statsKey = '';
  int _statsBaseUp = 0;
  int _statsBaseDown = 0;
  Timer? _statsTimer;

  /// Shows the address as "185.23.•.•" in the app, for screenshots.
  bool get hideIp => prefs['hide_ip'] == true;

  void setPref(String key, Object value) {
    prefs[key] = value;
    savePrefs?.call(prefs);
    _notify();
  }

  // ---------------------------------------------------------------- helpers

  int get nodeCount => subscriptions.fold(0, (n, s) => n + s.nodes.length);

  /// The selected server is in its subscription's automatic selection, so a
  /// server that stops answering is replaced by the next one by itself.
  bool get autoSwitching => subscriptionById(selection.subscription)?.auto.contains(selection.fingerprint) ?? false;

  Subscription? subscriptionById(String id) {
    for (final s in subscriptions) {
      if (s.id == id) return s;
    }
    return null;
  }

  /// Whether node [n] of [sub] is the selected one. Nodes that differ only
  /// in name share a fingerprint, so the name decides between them.
  bool isSelected(Subscription sub, NodeView n) {
    final sel = selection;
    if (sel.subscription != sub.id || sel.fingerprint != n.fingerprint) return false;
    if (n.name == sel.name) return true;
    // A stale name: the first node with the fingerprint stands in.
    final twins = sub.nodes.where((m) => m.fingerprint == n.fingerprint);
    return !twins.any((m) => m.name == sel.name) && identical(twins.first, n);
  }

  /// Whether the daemon knows setting [path]. An older daemon rejects the
  /// whole save when the UI sends a field it does not know, so controls for
  /// newer settings are hidden instead.
  bool hasSetting(String path) {
    dynamic v = settings;
    for (final k in path.split('.')) {
      if (v is! Map || !v.containsKey(k)) return false;
      v = v[k];
    }
    return true;
  }

  T setting<T>(String path, T fallback) {
    dynamic v = settings;
    for (final k in path.split('.')) {
      if (v is! Map) return fallback;
      v = v[k];
    }
    return v is T ? v : fallback;
  }

  void clearLogs() {
    logs.clear();
    _notify();
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _ipTimer?.cancel();
    _alerts.close();
    _events?.cancel();
    _retry?.cancel();
    _statusDebounce?.cancel();
    _statsTimer?.cancel();
    _subTimer?.cancel();
    _coreTimer?.cancel();
    super.dispose();
  }
}
