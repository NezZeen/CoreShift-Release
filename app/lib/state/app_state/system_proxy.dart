part of '../app_state.dart';

/// Each state's [SystemProxySync], made on first use: kept out of the
/// class's fields.
final _proxySyncs = Expando<SystemProxySync>('systemProxy');

/// Replaces `coreshiftd sysproxy` in tests: the arguments in, a report out.
@visibleForTesting
Future<ProxyReport> Function(List<String> args)? systemProxyRunner;

/// Replaces the look for a journal left by an earlier run, in tests.
@visibleForTesting
bool Function()? systemProxyJournal;

extension SystemProxyState on AppState {
  /// The mode without TUN in words, for the journal.
  String get _proxyModeName => platform.isAndroid
      ? 'прокси без VPN'
      : setting('proxy.system', false) == true
      ? 'системный прокси'
      : 'только прокси';

  /// The address the system proxy points at: the service's proxy port.
  String get systemProxyAddress => '127.0.0.1:${info.proxyPort}';

  SystemProxySync? get _proxySync {
    // Neither the demo nor Android sets a proxy of the system.
    final runner = systemProxyRunner ?? (platform.systemProxySupported && backend is! DemoBackend ? _runSystemProxy : null);
    if (runner == null) return null;
    return _proxySyncs[this] ??= SystemProxySync(
      run: runner,
      journalExists: systemProxyJournal ?? platform.systemProxyJournalExists,
      log: (text, warn) => _log(DateTime.now(), 'прокси', text, warn ? LogLevel.warn : LogLevel.ok),
    );
  }

  /// Whether the proxy of the system should point at CoreShift now: with
  /// «Системный прокси» on, while a connection without TUN is up. While
  /// one comes up or waits for the network it stays as it is (null): a
  /// reconnect closes the port only for a moment.
  bool? get _wantSystemProxy {
    final on = setting('proxy.system', false) == true && !status.tun;
    return switch (status.state) {
      ConnState.connected => on,
      ConnState.connecting || ConnState.noNetwork => on ? null : false,
      _ => false,
    };
  }

  /// Brings the proxy of the system in step with the connection; called
  /// on every change of the state (cheap when nothing changed).
  void _syncSystemProxy() {
    if (!loaded) return;
    final want = _wantSystemProxy;
    if (want == null) return;
    _proxySync?.update(want, systemProxyAddress);
  }

  /// Puts the proxy of the system back before the app quits, and waits
  /// for it: the guard would otherwise do it only after its grace.
  Future<void> settleSystemProxy() async {
    final sync = _proxySync;
    if (sync == null) return;
    sync.update(false, systemProxyAddress);
    await sync.pending;
  }
}

Future<ProxyReport> _runSystemProxy(List<String> args) async => ProxyReport.fromJson(await platform.runSystemProxy(args));
