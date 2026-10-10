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
part 'app_state/session_log.dart';
part 'app_state/checkup.dart';
part 'app_state/events.dart';
part 'app_state/journal.dart';
part 'app_state/connection.dart';
part 'app_state/core_updates.dart';
part 'app_state/app_update.dart';
part 'app_state/feedback.dart';
part 'app_state/address.dart';
part 'app_state/view.dart';
part 'app_state/selection.dart';

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
    this.askDisclaimer = false,
    this.journalSaver,
  }) : prefs = prefs ?? {};

  /// Saves the journal as a file to send (platform.saveJournal) and says
  /// where; null in tests, which save nothing.
  final Future<String?> Function(String name, String text)? journalSaver;

  /// Whether the window asks the user to accept the disclaimer until they
  /// do (ui/disclaimer.dart): the app does, a test's state does not.
  final bool askDisclaimer;

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

  /// «Проверить всё»: the last checkup, or the one running (checkup.dart).
  CheckupState checkup = const CheckupState();

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

  /// The connection the journal sums up when it ends (session_log.dart).
  final _session = _Session();

  /// The last state of the app's update the service told, to tell a check
  /// that found nothing.
  String _appUpdateWas = '';

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

  /// The user went to allow installing apps for an update.
  bool _installWhenAllowed = false;

  /// The downloaded update is offered in a window once per version while
  /// the app runs: "Позже" means until the next start.
  String _updateOffered = '';

  /// The error of the last failed state, so the same failure sent again as
  /// an "error" event (older services) is not told twice.
  String _failedWith = '';

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

  /// How many lines the journal keeps.
  static const logsKeep = 2000;

  /// How many of them may be the cores' plain output ([_chatter]).
  static const chatterKeep = 600;
  int _chatterLines = 0;

  /// The cores' and the TUN layer's output that is neither a warning nor an
  /// error: what a verbose journal adds, and the page shows only when
  /// searched for.
  static bool _chatter(LogLine l) => l.output && l.level == LogLevel.info;

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

  static String _layerText(String kind, String reason) => switch ((kind, reason)) {
    ('tun', 'up') => 'интерфейс поднят',
    ('tun', 'down') => 'интерфейс остановлен',
    ('dns', 'applied') => 'системный DNS направлен в туннель',
    ('dns', 'reverted') => 'системный DNS восстановлен',
    ('dns', 'network-changed') => 'сеть сменилась, прежний DNS недоступен — переподключение',
    _ => reason,
  };

  // ---------------------------------------------------------------- address

  /// The address sites see, once looked up; null before, or when the
  /// daemon is older than the lookup.
  IpInfo? publicIp;
  bool ipLoading = false;
  String ipError = '';
  bool ipUnsupported = false;
  String _ipKey = '';
  Timer? _ipTimer;

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
    _traffic.dispose();
    _speedNow.dispose();
    _shown.dispose();
    super.dispose();
  }
}
