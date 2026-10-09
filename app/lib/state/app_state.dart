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
import 'plural.dart';
import 'redact.dart';
import '../version.dart';

export 'import_link.dart' show ImportLink;
export 'plural.dart';

part 'app_state/models.dart';
part 'app_state/actions.dart';
part 'app_state/settings_diff.dart';
part 'app_state/servers.dart';
part 'app_state/traffic.dart';
part 'app_state/speed_test.dart';
part 'app_state/imports.dart';
part 'app_state/sub_alerts.dart';
part 'app_state/direct_hint.dart';

/// Everything the UI shows, kept in sync with the daemon through its event
/// stream. Widgets listen to it and call its actions.
class AppState extends ChangeNotifier {
  final Backend backend;

  /// UI preferences kept per user (theme, notifications); [savePrefs]
  /// stores them. Tests leave it out and so never touch the real file.
  final Json prefs;
  final Future<void> Function(Json prefs)? savePrefs;

  /// Runs the DNS leak test and returns the engine's result; by default
  /// the engine's (POST /v1/leaktest).
  final Future<Json> Function()? leakTestRunner;

  /// Opens links; by default in the browser, not at all on demo data.
  final Future<bool> Function(String url)? linkOpener;

  /// Asks Android for the VPN; by default [platform.prepareVpn], not at all
  /// on demo data.
  final Future<platform.VpnConsent> Function()? vpnConsent;

  /// Opens Android's VPN settings; by default [platform.openVpnSettings].
  final Future<bool> Function()? vpnSettingsOpener;

  /// How Android last refused the VPN, for the home page's way out: null
  /// once it allowed it, or the notice was put away.
  platform.VpnConsent? vpnRefusal;

  /// Android will not let CoreShift run its VPN: it refused, or turned the
  /// VPN off for another app. The home page offers its VPN settings.
  bool get vpnBlocked =>
      !status.active && (vpnRefusal != null || (status.state == ConnState.failed && status.error.toLowerCase().contains('android turned the vpn off')));

  /// Cores whose health checks fail right now, to log only the change.
  final _healthFailing = <String>{};

  /// How many checks in a row each core has failed; the first failure is
  /// often a blip, so the page speaks up from the second.
  final _healthStreak = <String, int>{};

  /// The connection is up, but the server does not answer: the check through
  /// the core in use keeps failing (when others did no better, the engine
  /// leaves the connection as it is, see the "no-better" event), or the
  /// engine found the server or the network at fault ([serverProblem]).
  bool get serverUnresponsive => status.state == ConnState.connected && ((_healthStreak[status.core] ?? 0) >= 2 || serverProblem.isNotEmpty);

  /// Who is at fault while nothing gets through, as the engine found it
  /// (see [Status.problem]); from its "server" event, or from the status
  /// when the app opens later. Empty otherwise.
  String get serverProblem => status.state == ConnState.connected ? (_serverProblem ?? status.problem) : '';
  String? _serverProblem;

  /// The engine's "direct" event came for this connection (see [directHint]);
  /// _directDismissed: the user put the hint off for this run of the app.
  bool _directBlocked = false;
  bool _directDismissed = false;

  /// Feeds an event as if the service had sent it, for tests.
  @visibleForTesting
  void injectEvent(Event e) => _onEvent(e);

  AppState(
    this.backend, {
    Json? prefs,
    this.savePrefs,
    this.leakTestRunner,
    this.linkOpener,
    this.vpnConsent,
    this.vpnSettingsOpener,
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

  /// The service's version for [versionMismatch]: two builds of one version
  /// would read «0.7.1» against «0.7.1», and the build number is not shown.
  String get serviceVersionLabel => info.version == version.version ? 'другая сборка' : info.buildVersion.label;

  /// What to do about [versionMismatch]. A service newer than the window is
  /// the usual case after an update with the window left open (a Linux
  /// package restarts only the service): restarting the window is enough.
  String get versionMismatchAdvice => info.buildVersion.compareTo(version) > 0
      ? 'Служба уже обновлена, а окно CoreShift открыто со старой сборки. Перезапустите CoreShift: «Выход» в меню значка, затем откройте снова.'
      : 'Служба не обновилась вместе с приложением. Переустановите CoreShift целиком.';

  bool online = false;
  bool loaded = false;
  String offlineReason = '';

  Status status = const Status();
  DaemonInfo info = const DaemonInfo();
  Json settings = {};
  List<Subscription> subscriptions = [];
  Selection selection = const Selection();

  /// The journal, oldest first.
  final List<LogLine> logs = [];

  /// Changes whenever [logs] does: the journal page filters it again only then.
  int logsRevision = 0;
  final List<int> latencies = []; // recent health checks, oldest first
  int swaps = 0;

  /// Bytes per second through the node, (up, down) per second, oldest first.
  final List<(int, int)> speed = [];
  int sessionUp = 0;
  int sessionDown = 0;
  static const speedKeep = 120;

  /// Notifies with every traffic sample, apart from the rest of the state:
  /// one a second while connected would otherwise redraw whatever page is
  /// open. What shows [speed] or the bytes moved listens to it. While the
  /// window is not [shown] it waits, and catches up once it is.
  Listenable get traffic => _traffic;
  final _traffic = ValueNotifier<int>(0);
  bool _trafficMissed = false;
  bool _sampleHidden = false;

  /// Notifies with every traffic sample, the window shown or not: the
  /// tray's tooltip, which is all there is to see of a hidden window.
  Listenable get speedNow => _speedNow;
  final _speedNow = ValueNotifier<int>(0);

  /// Whether the window is on screen ([setShown]). A desktop window hidden
  /// in the tray or minimized draws nothing: its animations stop
  /// ([TickerMode]), the speed and the graph wait, and the service samples
  /// the traffic every few seconds rather than every second.
  ValueListenable<bool> get shown => _shown;
  final _shown = ValueNotifier<bool>(true);

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
  }

  /// The cores' new versions are looked for by the app itself: first when
  /// it reaches the service, then once a day, and an hour after a check
  /// that failed for want of a network.
  Timer? _coreTimer;
  bool _coresChecked = false;

  /// When the connection last changed state, by the service's events: a
  /// check waits for it to settle.
  DateTime? _stateChangedAt;

  /// Automatic checks failed in a row; the journal hears of it from
  /// [_coreCheckFailuresToLog] on, as one failure is usually a network that
  /// is not there yet.
  int _coreCheckFailures = 0;

  static const _coreCheckEvery = Duration(hours: 24);
  static const _coreCheckRetry = Duration(hours: 1);
  static const _coreCheckSettle = Duration(seconds: 30);
  static const _coreCheckFailuresToLog = 3;

  /// When the next automatic check of the cores runs, from when it was set.
  @visibleForTesting
  Duration? coreCheckScheduled;

  /// Runs the automatic check of the cores now, as its timer would.
  @visibleForTesting
  Future<void> autoCoreUpdatesNow() => _autoCoreUpdates();

  void _scheduleCoreCheck(Duration after) {
    _coreTimer?.cancel();
    coreCheckScheduled = after;
    if (_disposed) return;
    _coreTimer = Timer(after, () => unawaited(_autoCoreUpdates()));
  }

  /// How long the automatic check has to wait. Not while the service
  /// connects or disconnects, nor in the first seconds of a connection:
  /// a check started then, as the app opens with a service that connects
  /// by itself, failed for every core at once.
  Duration _coreCheckWait() {
    if (!online || busy || checkingUpdates || updatingCore.isNotEmpty) return const Duration(minutes: 1);
    if (status.state == ConnState.connecting || status.state == ConnState.disconnecting) return const Duration(seconds: 5);
    // The status the app loaded tells it before the events do.
    final changed = [_stateChangedAt, status.since].nonNulls.fold<DateTime?>(null, (a, b) => a == null || b.isAfter(a) ? b : a);
    final since = changed == null ? null : DateTime.now().difference(changed);
    if (since != null && since < _coreCheckSettle) return _coreCheckSettle - since;
    return Duration.zero;
  }

  /// Looks for newer cores and installs them, without a word. A check that
  /// fails is tried again in an hour, and only one that keeps failing is
  /// told, in one line of the journal for every core. Android updates its
  /// cores with the app.
  Future<void> _autoCoreUpdates() async {
    if (platform.isAndroid || _disposed) return;
    _coresChecked = true;
    final wait = _coreCheckWait();
    if (wait > Duration.zero) {
      _scheduleCoreCheck(wait);
      return;
    }
    final failed = await checkCoreUpdates();
    final temporary = failed.values.any(coreCheckTemporary);
    if (failed.isEmpty) {
      _coreCheckFailures = 0;
    } else if (++_coreCheckFailures == _coreCheckFailuresToLog) {
      _log(DateTime.now(), 'ядра', coreCheckFailedText(failed, _coreCheckFailures, retryInHour: temporary), LogLevel.warn);
    }
    await installCoreUpdates();
    _scheduleCoreCheck(temporary ? _coreCheckRetry : _coreCheckEvery);
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
  /// On Linux a newer version is only announced ('available'), to be
  /// downloaded from its page.
  bool get offerUpdate =>
      appUpdate.label != _updateOffered &&
      ((appUpdate.state == 'ready' && (platform.isAndroid || appUpdate.waiting || !setting('app_update.auto', true))) || appUpdate.state == 'available');

  /// Opens the page of an announced update (Linux) in the browser; false
  /// when there is none or it could not be opened. Only GitHub (or GitLab mirror) release
  /// pages, which the daemon has checked too.
  Future<bool> openUpdatePage() async {
    final url = appUpdate.url;
    if (!isReleasePage(url)) return false;
    return platform.openUrl(url);
  }

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
        _stateChangedAt = e.time;
        if (e.state == 'connecting') {
          _healthFailing.clear();
          _healthStreak.clear();
          _directBlocked = false;
        }
        if (e.state != 'connected') _serverProblem = null;
        // Without a network the "network" event beside it says what goes on.
        if (e.state != 'no-network') {
          _log(
            e.time,
            'служба',
            _stateText(e.state) + (e.error.isNotEmpty ? ': ${journalError(e.error)}' : ''),
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
        // A core update found while connected waits for the VPN to be off.
        if (e.state == 'idle' && live && coreUpdatesWaiting.isNotEmpty) Timer(const Duration(seconds: 2), installCoreUpdates);
        if (e.state == 'failed' && live) {
          toast(humanError(e.error), ToastKind.err);
          _alerts.add(Alert('VPN отключился', humanError(e.error)));
        }
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
        _log(e.time, e.source, e.line, _outputLevel(e.line), output: true);
      case 'app-update':
        _onAppUpdate(e, live);
      case 'tun' when e.reason == 'retry':
        // The failed attempt's own FATAL line is in the journal just above:
        // this one says the next attempt follows.
        _log(e.time, 'TUN', 'интерфейс не поднялся: Windows ещё убирает прежний адаптер; повтор через ${e.line}. Причина: ${e.error}', LogLevel.warn);
      case 'dns' when e.reason == 'network-changed':
        // The reconnect that follows says why it happens.
        _log(e.time, 'сеть', e.line.isNotEmpty ? e.line : _layerText(e.kind, e.reason), LogLevel.swap);
        if (live) toast('Сеть сменилась — CoreShift переподключается');
      case 'tun':
      case 'dns':
        _log(e.time, e.kind.toUpperCase(), e.error.isNotEmpty ? e.error : _layerText(e.kind, e.reason), e.error.isNotEmpty ? LogLevel.warn : LogLevel.info);
      case 'action':
        // Why the connection changes, when not by the window's buttons:
        // Android's tile and notification, "Автозапуск", the service itself.
        _log(e.time, e.source.isEmpty ? 'действие' : e.source, e.line, LogLevel.info);
      case 'error':
        // Services before 0.7.2 sent a failure twice: in the state and here.
        if (e.error == _failedWith) break;
        _log(e.time, 'служба', journalError(e.error), LogLevel.err);
        if (live) toast(humanError(e.error), ToastKind.err);
      case 'network':
        _onNetworkEvent(e, live);
      case 'traffic':
        // Hidden, the samples come every few seconds: the graph, a sample a
        // second, keeps only the latest of them rather than squeezing
        // minutes into its two.
        if (!_shown.value && _sampleHidden && speed.isNotEmpty) speed.removeLast();
        _sampleHidden = !_shown.value;
        speed.add((e.upRate, e.downRate));
        if (speed.length > speedKeep) speed.removeRange(0, speed.length - speedKeep);
        sessionUp = e.up;
        sessionDown = e.down;
        if (_disposed) break;
        _speedNow.value++;
        if (_shown.value) {
          _traffic.value++;
        } else {
          _trafficMissed = true;
        }
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

  /// The error of the last failed state, so the same failure sent again as
  /// an "error" event (older services) is not told twice.
  String _failedWith = '';

  /// The device lost its network or got it back (engine/internal/service/
  /// netwatch.go): the journal says so in words, a toast too as it happens.
  void _onNetworkEvent(Event e, bool live) {
    final (text, level, note) = switch (e.reason) {
      'waiting' => ('сети нет: подключусь, как только она появится', LogLevel.warn, 'Нет сети — CoreShift подключится, когда она появится'),
      'lost' => ('сеть пропала: VPN ждёт её, ядра и сервер не меняются', LogLevel.warn, 'Пропала сеть — VPN подождёт её'),
      'back' => ('сеть вернулась', LogLevel.ok, 'Сеть вернулась'),
      'reconnect' => ('сеть вернулась, но связь через сервер не восстановилась: переподключаюсь', LogLevel.swap, ''),
      _ => (e.line.isNotEmpty ? e.line : e.reason, LogLevel.info, ''),
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
    final (title, body) = serverProblemText(e.reason, e.from);
    _log(e.time, 'сервер', '$title${e.line.isEmpty ? '' : ' (${e.line})'}', LogLevel.err);
    if (live) {
      toast(title, ToastKind.err);
      // Out of the window's sight: the desktop's tray, the phone's
      // notification, which opens the app where the banner offers the way out.
      _alerts.add(Alert(title, body));
      platform.notify(title, body);
    }
    _notify();
  }

  /// The heading and the advice for a [Status.problem] of server [name].
  static (String, String) serverProblemText(String problem, String name) {
    final server = name.isEmpty ? 'Сервер' : 'Сервер «$name»';
    return switch (problem) {
      'server-down' => ('$server не отвечает', 'Интернет работает, а сервер нет: он выключен или заблокирован. Выберите другой сервер.'),
      'offline' => ('Нет связи с интернетом', 'Не отвечают ни сервер, ни известные сайты: дело в сети, а не в VPN. Проверьте Wi-Fi или мобильный интернет.'),
      'server-up' => (
        name.isEmpty ? 'VPN через сервер не работает' : 'VPN через «$name» не работает',
        'Сервер на связи, но соединение через него не проходит: его блокируют или изменились его настройки. Обновите подписку или выберите другой сервер.',
      ),
      'unknown' => (
        name.isEmpty ? 'Связь через сервер не проходит' : 'Связь через «$name» не проходит',
        'Не удалось выяснить, виноват сервер или сеть. Проверьте интернет; если он работает — выберите другой сервер или переподключитесь.',
      ),
      // Checks fail, and the engine has not looked why yet.
      _ => ('$server не отвечает', 'Связь через него не проходит: он недоступен или заблокирован. Выберите другой сервер или переподключитесь.'),
    };
  }

  /// Adds a line to the journal, in the order of time rather than of
  /// arrival: the app's own first lines (the update notice) come before the
  /// service's replay of what happened earlier. A line already there, the
  /// same event replayed when the event stream reconnects, is not added
  /// twice. The output of cores and of the TUN layer comes in bursts, so it
  /// redraws only now and then.
  void _log(DateTime t, String source, String msg, LogLevel level, {bool output = false}) {
    // Nearly always the end: the walk back is over the few later lines.
    var i = logs.length;
    while (i > 0 && logs[i - 1].time.isAfter(t)) {
      i--;
    }
    for (var j = i - 1; j >= 0 && logs[j].time.isAtSameMomentAs(t); j--) {
      if (logs[j].source == source && logs[j].message == msg) return;
    }
    logs.insert(i, LogLine(t, source, msg, level, output: output));
    if (logs.length > logsKeep) logs.removeRange(0, logs.length - logsKeep);
    logsRevision++;
    if (!output || logsRevision % 20 == 0) _notify();
  }

  /// How many lines the journal keeps.
  static const logsKeep = 2000;

  static String _stateText(String s) => switch (s) {
    'connecting' => 'подключение…',
    'no-network' || 'noNetwork' => 'нет сети, жду её',
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
    if (e.error.isNotEmpty) _log(e.time, 'обновление', journalError(e.error), LogLevel.warn);
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

  /// Shows [message] for a few seconds; one with an [action] («Отменить»)
  /// stays a little longer, to give time to press it.
  void toast(String message, [ToastKind kind = ToastKind.info, (String, VoidCallback)? action]) {
    // A failed connect reports its error twice: as the response and as an
    // event of the daemon.
    if (toasts.any((t) => t.message == message)) return;
    final t = Toast(++_toastSeq, message, kind, action: action);
    toasts.add(t);
    _notify();
    Timer(Duration(seconds: action == null ? 5 : 8), () {
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

  /// Shows the address as "203.0.•.•" in the app, for screenshots.
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
    logsRevision++;
    _notify();
  }

  void _notify() {
    if (!_disposed) notifyListeners();
  }

  /// Says whether the window is on screen: DesktopFrame, from the window's
  /// events. Shown again, the speed and the graph are brought up to date at
  /// once, and so is the service's pace.
  void setShown(bool v) {
    if (_disposed || _shown.value == v) return;
    _shown.value = v;
    if (v && _trafficMissed) {
      _trafficMissed = false;
      _traffic.value++;
    }
    if (online) unawaited(_sendView());
  }

  /// Tells the service whether this window is hidden (POST /v1/view), so
  /// that it samples the traffic seldom while no window shows it.
  Future<void> _sendView() async {
    final view = backend.view;
    if (view.isEmpty || _disposed) return;
    try {
      await backend.call('POST', '/v1/view', {'view': view, 'hidden': !_shown.value});
    } catch (_) {
      // A service before 0.8.3, or its stream just reopening: it samples
      // at its usual pace, which costs it little.
    }
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
    _traffic.dispose();
    _speedNow.dispose();
    _shown.dispose();
    super.dispose();
  }
}
