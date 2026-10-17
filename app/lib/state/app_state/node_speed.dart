part of '../app_state.dart';

/// From this many megabits per second a server is fast, from the lower one
/// it is fair; slower is slow. The list colours the figures by them.
const speedGoodMbps = 30.0;
const speedFairMbps = 10.0;

/// What a speed test of some servers would cost, for the question a phone
/// asks first.
class SpeedPlan {
  final int servers;
  final int megabytesEach;

  /// The device is on mobile data, where traffic costs money.
  final bool mobile;
  const SpeedPlan({required this.servers, required this.megabytesEach, this.mobile = false});

  int get megabytes => servers * megabytesEach;

  /// «30 серверов × 10 МБ ≈ 300 МБ трафика» (from a gigabyte on, in gigabytes).
  String get estimate {
    final total = megabytes >= 1000 ? tr('servers.unit.gb', {'n': (megabytes / 1000).toStringAsFixed(1)}) : tr('servers.unit.mb', {'n': megabytes});
    return tr('servers.speed.estimate', {'servers': servers, 'each': megabytesEach, 'total': total});
  }
}

/// The speed test of the servers in the list: the service downloads through
/// each server in turn (POST /v1/speedtest/nodes), and its "nodespeed"
/// events bring the figures as they come. Auto-select and failover go by
/// latency alone and never read these figures.
extension AppStateNodeSpeed on AppState {
  /// Whether the service has the test: its settings have the volume.
  bool get speedSupported => hasSetting('cores.speed_mb');

  /// How much one server may download, in megabytes (5, 10 or 25).
  int get speedVolumeMB => setting<num>('cores.speed_mb', 10).toInt();

  NodeSpeed? speedOf(String subscription, String fingerprint) => nodeSpeeds['$subscription/$fingerprint'];

  /// What testing [servers] servers would cost.
  Future<SpeedPlan> speedPlan(int servers) async {
    var mobile = false;
    try {
      mobile = ((await backend.call('GET', '/v1/speedtest/nodes')) as Json)['mobile'] == true;
    } catch (_) {}
    return SpeedPlan(servers: servers, megabytesEach: speedVolumeMB, mobile: mobile);
  }

  /// Tests [servers], the list the page shows: all of them in the
  /// subscription [subscription], or whatever [fingerprints] names.
  /// Progress and results arrive as events.
  Future<void> testNodeSpeed({required int servers, String? subscription, List<String>? fingerprints}) async {
    if (testingSpeed || testingLatency || servers == 0) return;
    testingSpeed = true;
    speedDone = 0;
    speedTotal = servers;
    speedBytes = 0;
    speedCurrent = null;
    _notify();
    _logAction(tr('servers.speed.log.start', {'servers': servers, 'mb': speedVolumeMB}));
    final ok = await _act(() async {
      await backend.call('POST', '/v1/speedtest/nodes', {'subscription': ?subscription, 'nodes': ?fingerprints, 'bytes': speedVolumeMB << 20});
    });
    if (!ok) {
      testingSpeed = false;
      _notify();
    }
  }

  /// Ends the running test; the figures so far stay.
  Future<void> stopNodeSpeed() async {
    await _act(() async {
      await backend.call('DELETE', '/v1/speedtest/nodes');
    });
  }

  /// Takes up a test that is already running, as when the app was closed
  /// and opened again meanwhile.
  Future<void> syncNodeSpeed() async {
    if (!speedSupported) return;
    try {
      final st = (await backend.call('GET', '/v1/speedtest/nodes')) as Json;
      testingSpeed = st['running'] == true;
      speedDone = (st['done'] as num?)?.toInt() ?? 0;
      speedTotal = (st['total'] as num?)?.toInt() ?? 0;
      speedBytes = (st['bytes'] as num?)?.toInt() ?? 0;
      if (!testingSpeed) speedCurrent = null;
      _notify();
    } catch (_) {}
  }

  void _onNodeSpeedEvent(Event e) {
    final key = '${e.subscription}/${e.fingerprint}';
    switch (e.reason) {
      case 'started':
        testingSpeed = true;
        speedDone = 0;
        speedBytes = 0;
        speedTotal = e.total;
        speedCurrent = null;
      case 'testing':
        speedCurrent = key;
      case 'node':
        nodeSpeeds[key] = NodeSpeed(mbps: e.mbps, bytes: e.bytes, source: e.source, error: e.error, errorCode: e.code, errorArgs: e.args, at: e.time);
        if (speedDone < speedTotal) speedDone++;
        speedBytes += e.bytes;
        if (speedCurrent == key) speedCurrent = null;
      case 'done' || 'stopped':
        testingSpeed = false;
        speedCurrent = null;
        speedBytes = e.bytes;
        final done = e.reason == 'done';
        final traffic = _traffic(e.bytes);
        final text = tr(done ? 'servers.speed.log.done' : 'servers.speed.log.stopped', {'done': speedDone, 'total': speedTotal, 'traffic': traffic});
        _log(e.time, tr('state.src.speed'), text, LogLevel.ok);
        if (!e.time.isBefore(_liveSince)) {
          toast(tr(done ? 'servers.speed.toast.done' : 'servers.speed.toast.stopped', {'traffic': traffic}), ToastKind.info);
        }
    }
    _notify();
  }

  static String _traffic(int bytes) => bytes >= 1e9
      ? tr('servers.unit.gb', {'n': (bytes / 1e9).toStringAsFixed(1)})
      : tr('servers.unit.mb', {'n': (bytes / 1e6).toStringAsFixed(bytes >= 1e7 ? 0 : 1)});
}
