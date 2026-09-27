import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';

import '../api/backend.dart';
import '../api/demo_backend.dart';
import '../api/models.dart';
import '../platform/platform.dart' as platform;
import 'errors.dart';
import 'leak.dart';
import '../version.dart';

enum LogLevel { info, ok, warn, err, swap }

class LogLine {
  final DateTime time;
  final String source;
  final String message;
  final LogLevel level;
  const LogLine(this.time, this.source, this.message, this.level);
}

enum ToastKind { info, ok, err, swap }

class Toast {
  final int id;
  final String message;
  final ToastKind kind;
  const Toast(this.id, this.message, this.kind);
}

/// Something worth a system notification when the window is out of sight.
class Alert {
  final String title;
  final String body;
  const Alert(this.title, this.body);
}

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

  AppState(this.backend, {Json? prefs, this.savePrefs, this.leakTestRunner, this.linkOpener, this.version = BuildVersion.app}) : prefs = prefs ?? {};

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
  final List<int> pings = []; // recent pings of the connected server, oldest first
  String pingMethod = ''; // how the last ping was measured: icmp or tcp
  String pingError = ''; // why the server did not answer the last ping
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
      _notify();
    } catch (e) {
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
    offlineReason = reason is DaemonOffline || reason is ApiError ? '$reason' : 'Служба CoreShift не отвечает';
    _notify();
    _retry?.cancel();
    _retry = Timer(const Duration(seconds: 2), _connectDaemon);
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
    subscriptions = _subs(results[3]);
    selection = Selection.fromJson(results[4] as Json);
    loaded = true;
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
      appUpdate.state == 'ready' &&
      appUpdate.label != _updateOffered &&
      (platform.isAndroid || appUpdate.waiting || !setting('app_update.auto', true));

  void updateOffered() => _updateOffered = appUpdate.label;

  /// Opens a link the panel sent (its support chat, the subscription's
  /// page). Only web and Telegram links: another scheme could start a
  /// program. The link is not shown, it may carry the subscription's token.
  Future<void> openLink(String url) async {
    final u = Uri.tryParse(url.trim());
    final ok = u != null && (u.scheme == 'tg' || ((u.scheme == 'https' || u.scheme == 'http') && u.host.isNotEmpty));
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
      } else if (what == 'selection') {
        selection = Selection.fromJson(await backend.call('GET', '/v1/selection') as Json);
      } else {
        final r = await Future.wait([backend.call('GET', '/v1/subscriptions'), backend.call('GET', '/v1/selection')]);
        subscriptions = _subs(r[0]);
        selection = Selection.fromJson(r[1] as Json);
      }
      _notify();
    } catch (_) {}
  }

  void _onEvent(Event e) {
    final live = !e.time.isBefore(_liveSince);
    switch (e.kind) {
      case 'state':
        _log(
          e.time,
          'служба',
          _stateText(e.state) + (e.error.isNotEmpty ? ': ${e.error}' : ''),
          e.state == 'failed' ? LogLevel.err : (e.state == 'connected' ? LogLevel.ok : LogLevel.info),
        );
        if (e.state == 'connecting') {
          latencies.clear();
          pings.clear();
          pingMethod = pingError = '';
          swaps = 0;
          speed.clear();
          sessionUp = sessionDown = 0;
        }
        if (e.state == 'idle' || e.state == 'failed') speed.clear();
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
      case 'core-failed':
        _log(e.time, e.core, 'отключено (${_reasonText(e.reason)}): ${e.error}', LogLevel.err);
        _statusSoon();
      case 'health':
        if (e.probe) break;
        if (e.error.isNotEmpty) {
          _log(e.time, e.core, 'проверка связи не прошла: ${e.error}', LogLevel.warn);
        } else {
          latencies.add(e.latencyMs);
          if (latencies.length > 60) latencies.removeAt(0);
          _notify();
        }
      case 'ping':
        if (e.error.isNotEmpty) {
          if (pingError.isEmpty) _log(e.time, 'пинг', 'сервер не отвечает: ${e.error}', LogLevel.warn);
          pingError = e.error;
        } else {
          pingError = '';
          pingMethod = e.method;
          pings.add(e.latencyMs);
          if (pings.length > 60) pings.removeAt(0);
        }
        _notify();
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
        } else if (e.reason == 'finished') {
          testingLatency = false;
        } else if (e.fingerprint.isNotEmpty) {
          latency['${e.subscription}/${e.fingerprint}'] = Latency(ms: e.latencyMs, error: e.error, core: e.core, method: e.method);
        }
        _notify();
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
      'Сейчас: ${_stateText(status.state.name)}${status.core.isEmpty ? '' : ' через ${status.core}'}'
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
      toast('${coreName(kind)} обновлён до $v${status.active ? '. Новая версия заработает после переподключения' : ''}', ToastKind.ok);
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

  void setPref(String key, Object value) {
    prefs[key] = value;
    savePrefs?.call(prefs);
    _notify();
  }

  // ---------------------------------------------------------------- helpers

  int get nodeCount => subscriptions.fold(0, (n, s) => n + s.nodes.length);

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
    _alerts.close();
    _events?.cancel();
    _retry?.cancel();
    _statusDebounce?.cancel();
    super.dispose();
  }
}

/// What differs between two settings objects, one entry per changed value:
/// "tun: да → нет", "routing.direct_domains: 3 → 4 записей". Lists are
/// counted, not listed: they can be long.
List<String> settingsChanges(Map before, Map after, [String prefix = '']) {
  String show(Object? v) => switch (v) {
    true => 'да',
    false => 'нет',
    null => '—',
    List l => '${l.length} записей',
    String s when s.isEmpty => '«»',
    _ => '$v',
  };
  final out = <String>[];
  for (final k in {...before.keys, ...after.keys}) {
    final a = before[k], b = after[k];
    final path = '$prefix$k';
    if (a is Map && b is Map) {
      out.addAll(settingsChanges(a, b, '$path.'));
    } else if (a is List && b is List) {
      if (jsonEncode(a) != jsonEncode(b)) {
        out.add(a.length == b.length ? '$path: изменён список (${b.length} записей)' : '$path: ${show(a)} → ${show(b)}');
      }
    } else if (a != b) {
      out.add('$path: ${show(a)} → ${show(b)}');
    }
  }
  return out;
}
