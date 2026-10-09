import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/state/errors.dart';

/// The demo service, whose cores' update check fails for want of a
/// network, as it did for every core at once right after an update.
class _NoGitHub extends DemoBackend {
  int checks = 0;

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    if (path == '/v1/cores/updates') {
      checks++;
      return [
        for (final k in ['xray', 'sing-box', 'mihomo'])
          {
            'kind': k,
            'current': '1.0.0',
            'available': false,
            'error': '$k: check for updates: Get "https://api.github.com/repos/x/releases/latest": dial tcp: connectex: No connection could be made',
          },
      ];
    }
    return super.call(method, path, body);
  }
}

/// The demo service, whose newer sing-box appears only once [offer] is set:
/// the check as the app starts finds nothing.
class _LateRelease extends DemoBackend {
  bool offer = false;

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    if (path == '/v1/cores/updates' && !offer) {
      return [
        for (final k in ['xray', 'sing-box', 'mihomo']) {'kind': k, 'current': '1.0.0', 'latest': '1.0.0', 'available': false},
      ];
    }
    return super.call(method, path, body);
  }
}

void main() {
  test('a core update found while the VPN is on is installed at once, and applied without a reconnect', () async {
    final backend = _LateRelease();
    final state = AppState(backend);
    addTearDown(state.dispose);
    state.start();
    for (var i = 0; i < 100 && !state.loaded; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    await state.connect();
    for (var i = 0; i < 200 && state.status.state != ConnState.connected; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    expect(state.status.state, ConnState.connected);
    backend.offer = true;
    await state.checkCoreUpdates();
    expect(state.coreUpdatesWaiting, ['sing-box']);

    // Not while a connection is going down.
    final connected = state.status;
    state.status = const Status(state: ConnState.disconnecting);
    await state.installCoreUpdates();
    expect(state.coreUpdatesWaiting, ['sing-box']);
    state.status = connected;

    await state.installCoreUpdates();
    expect(state.coreUpdatesWaiting, isEmpty);
    // The events, and the status they ask for, come a moment later.
    await Future.delayed(const Duration(milliseconds: 500));
    List<String> told() => [for (final l in state.logs.where((l) => l.source == 'sing-box')) l.message];
    expect(told(), containsAll(['обновлено до 1.14.3', 'обновление до 1.14.3 применено без отключения VPN']));
    expect(state.status.state, ConnState.connected);
    // Not a change of settings: nothing asks to reconnect.
    expect(state.status.settingsPending, isFalse);
    expect(state.toasts, isEmpty);
    await state.disconnect();
  });

  test('a core update check that keeps failing is told once, in one line, and retried in an hour', () async {
    final backend = _NoGitHub();
    final state = AppState(backend);
    addTearDown(state.dispose);
    state.start();
    // The first check runs as soon as the service is reached.
    for (var i = 0; i < 200 && (backend.checks == 0 || state.checkingUpdates || state.coreCheckScheduled == null); i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    expect(backend.checks, 1);
    expect(state.coreCheckScheduled, const Duration(hours: 1));

    bool aboutCheck(LogLine l) => l.message.contains('проверить обновления') || l.message.contains('узнать о нов');
    List<String> told() => [for (final l in state.logs.where(aboutCheck)) '${l.source}: ${l.message}'];
    // A second failure is still not news.
    await state.autoCoreUpdatesNow();
    expect(told(), isEmpty);
    // The third is, in one line for all the cores.
    await state.autoCoreUpdatesNow();
    expect(told(), ['ядра: не удалось проверить обновления (xray, sing-box, mihomo) 3 раза подряд: нет связи с GitHub. Следующая попытка через час']);
    expect(state.logs.where(aboutCheck).single.level, LogLevel.warn);
    // And only once while it keeps failing; never a toast.
    await state.autoCoreUpdatesNow();
    expect(told(), hasLength(1));
    expect(state.toasts, isEmpty);
    expect(state.coreCheckScheduled, const Duration(hours: 1));
    expect(backend.checks, 4);
  });

  test('the reason of a failed check reads as the end of a sentence', () {
    expect(coreCheckReason('xray: check for updates: dial tcp: i/o timeout'), 'GitHub не ответил вовремя');
    expect(coreCheckReason('xray: check for updates: EOF'), 'нет связи с GitHub');
    // A refusal through the proxy is not hidden behind the direct attempt's error.
    expect(
      coreCheckReason('through the proxy: xray: check for updates: GitHub answered 403 Forbidden; directly: xray: check for updates: EOF'),
      'GitHub временно ограничил проверки',
    );
    expect(coreCheckTemporary('xray: check for updates: EOF'), isTrue);
    expect(coreCheckTemporary('Служба CoreShift не запущена'), isTrue);
    expect(coreCheckTemporary('mihomo: no release build for linux/riscv64'), isFalse);
    expect(coreCheckFailedText({'': 'Служба CoreShift не запущена'}, 3, retryInHour: true), startsWith('не удалось проверить обновления 3 раза подряд: '));
  });
}
