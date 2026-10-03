part of '../app_state.dart';

enum SpeedPhase { idle, latency, download, upload, done, failed }

/// The speed test as the home page shows it: while it runs, its phase and
/// the live rate; afterwards the result. Rates are bytes per second.
class SpeedTestState {
  final SpeedPhase phase;
  final int latencyMs;
  final int downBps;
  final int upBps;

  /// Measured through the VPN server [server], else without the VPN.
  final bool vpn;
  final String server;
  final String error;

  /// Who measured: a speedtest.net server's sponsor, or "Cloudflare", and
  /// the speedtest.net server's city and country.
  final String testServer;
  final String testCity;
  final String testCountry;

  /// When the result came.
  final DateTime? at;

  const SpeedTestState({
    this.phase = SpeedPhase.idle,
    this.latencyMs = 0,
    this.downBps = 0,
    this.upBps = 0,
    this.vpn = false,
    this.server = '',
    this.error = '',
    this.testServer = '',
    this.testCity = '',
    this.testCountry = '',
    this.at,
  });

  bool get running => phase == SpeedPhase.latency || phase == SpeedPhase.download || phase == SpeedPhase.upload;

  /// The server that measured, as the result names it: "Helsinki, Elisa",
  /// or "Cloudflare"; empty from an older service.
  String get testServerName => [testCity, testServer].where((s) => s.isNotEmpty).join(', ');

  SpeedTestState copyWith({SpeedPhase? phase, int? latencyMs, int? downBps, int? upBps}) => SpeedTestState(
    phase: phase ?? this.phase,
    latencyMs: latencyMs ?? this.latencyMs,
    downBps: downBps ?? this.downBps,
    upBps: upBps ?? this.upBps,
    vpn: vpn,
    server: server,
  );
}

/// The speed test runs in the service, through the core while connected;
/// its events show the rate as it goes.
extension AppStateSpeedTest on AppState {
  Future<void> runSpeedTest() async {
    if (speedTest.running || !online) return;
    final vpn = status.state == ConnState.connected;
    speedTest = SpeedTestState(phase: SpeedPhase.latency, vpn: vpn, server: vpn ? status.node : '');
    _notify();
    _logAction('тест скорости ${vpn ? 'через «${status.node}»' : 'без VPN'}');
    try {
      final r = SpeedResult.fromJson(await backend.call('POST', '/v1/speedtest') as Json);
      speedTest = SpeedTestState(
        phase: SpeedPhase.done,
        latencyMs: r.latencyMs,
        downBps: r.downloadBps,
        upBps: r.uploadBps,
        vpn: r.vpn,
        server: r.server,
        testServer: r.testServer,
        testCity: r.testCity,
        testCountry: r.testCountry,
        at: DateTime.now(),
      );
      final where = speedTest.testServerName.isEmpty ? '' : ', сервер теста ${[speedTest.testServerName, r.testCountry].where((s) => s.isNotEmpty).join(', ')}';
      _log(DateTime.now(), 'скорость', 'загрузка ${_mbit(r.downloadBps)}, отдача ${_mbit(r.uploadBps)}, задержка ${r.latencyMs} мс$where', LogLevel.ok);
    } on ApiError catch (e) {
      if (e.status == 404 || e.status == 405) speedUnsupported = true;
      speedTest = SpeedTestState(phase: SpeedPhase.failed, vpn: vpn, error: e.status == 409 ? 'Тест скорости уже идёт' : speedTestError(e.message));
      _log(DateTime.now(), 'скорость', 'тест не удался: ${e.message}', LogLevel.err);
    } catch (e) {
      speedTest = SpeedTestState(phase: SpeedPhase.failed, vpn: vpn, error: speedTestError('$e'));
      if (e is DaemonOffline) _lost(e);
    }
    _notify();
  }

  /// The live rate of the running phase; the response of the request has
  /// the final figures.
  void _onSpeedEvent(Event e) {
    if (!speedTest.running) return;
    speedTest = switch (e.reason) {
      'latency' => speedTest.copyWith(phase: SpeedPhase.download, latencyMs: e.latencyMs),
      'download' => speedTest.copyWith(phase: SpeedPhase.download, downBps: e.downRate),
      'upload' => speedTest.copyWith(phase: SpeedPhase.upload, upBps: e.upRate),
      _ => speedTest,
    };
    _notify();
  }

  static String _mbit(int bps) => '${(bps * 8 / 1e6).toStringAsFixed(1)} Мбит/с';
}
