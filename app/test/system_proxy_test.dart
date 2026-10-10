import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/state/system_proxy_sync.dart';

/// A `coreshiftd sysproxy` that records its calls and answers as the
/// engine's does on Windows.
class _FakeSysProxy {
  final calls = <List<String>>[];
  bool journal = false;
  List<String> backends = ['windows'];

  Future<ProxyReport> run(List<String> args) async {
    calls.add(args);
    await Future<void>.delayed(const Duration(milliseconds: 5));
    if (args.first == 'apply') {
      journal = backends.isNotEmpty;
      return ProxyReport(
        action: 'apply',
        backends: backends,
        applied: backends,
        pending: journal,
        errors: backends.isEmpty ? ['no supported desktop'] : const [],
      );
    }
    final had = journal;
    journal = false;
    return ProxyReport(action: args.first, backends: backends, restored: had ? backends : const []);
  }
}

Future<void> _settle(SystemProxySync s) async {
  for (var i = 0; i < 20 && s.pending != null; i++) {
    await s.pending;
    await Future<void>.delayed(Duration.zero);
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('SystemProxySync', () {
    late _FakeSysProxy fake;
    late List<(String, bool)> lines;
    late SystemProxySync sync;

    setUp(() {
      fake = _FakeSysProxy();
      lines = [];
      sync = SystemProxySync(run: fake.run, journalExists: () => fake.journal, log: (t, w) => lines.add((t, w)));
    });

    test('sets the proxy to the port and puts it back', () async {
      sync.update(true, '127.0.0.1:17890');
      await _settle(sync);
      expect(fake.calls, [
        ['apply', '-addr', '127.0.0.1:17890'],
      ]);
      expect(lines.single.$1, startsWith('системный прокси включён (Windows)'));
      expect(lines.single.$2, isFalse);
      // The same wish again does nothing.
      sync.update(true, '127.0.0.1:17890');
      await _settle(sync);
      expect(fake.calls, hasLength(1));
      sync.update(false, '127.0.0.1:17890');
      await _settle(sync);
      expect(fake.calls.last, ['restore']);
      expect(lines.last.$1, 'системный прокси выключен, прежние настройки восстановлены (Windows)');
    });

    test('nothing to put back at the start, nothing run', () async {
      sync.update(false, '127.0.0.1:17890');
      await _settle(sync);
      expect(fake.calls, isEmpty);
      expect(lines, isEmpty);
    });

    test('a journal left by a crashed run is restored at the start', () async {
      fake.journal = true;
      sync.update(false, '127.0.0.1:17890');
      await _settle(sync);
      expect(fake.calls, [
        ['restore'],
      ]);
      expect(lines.single.$1, contains('прежние настройки восстановлены'));
    });

    test('a change while one runs comes after it, the latest only', () async {
      sync.update(true, '127.0.0.1:17890');
      sync.update(false, '127.0.0.1:17890');
      sync.update(true, '127.0.0.1:17890');
      await _settle(sync);
      expect(fake.calls, [
        ['apply', '-addr', '127.0.0.1:17890'],
      ]);
    });

    test('a desktop it cannot set is told, as a warning', () async {
      fake.backends = [];
      sync.update(true, '127.0.0.1:17890');
      await _settle(sync);
      expect(lines.single.$1, contains('рабочий стол не поддерживается'));
      expect(lines.single.$1, contains('127.0.0.1:17890'));
      expect(lines.single.$2, isTrue);
    });
  });

  test('a report from the engine reads as the journal says it', () {
    final r = ProxyReport.fromJson({
      'action': 'restore',
      'backends': ['gnome', 'kde'],
      'restored': ['gnome'],
      'changed': ['kde'],
      'errors': <String>[],
      'pending': false,
    });
    final lines = proxyReportLines(r, address: '127.0.0.1:17890').map((l) => l.$1).toList();
    expect(lines, ['системный прокси выключен, прежние настройки восстановлены (GNOME)', 'системный прокси уже изменён вручную (KDE): оставлен как есть']);
  });

  // The app follows the connection: the proxy is set once connected
  // without TUN with «Системный прокси» on, and put back on disconnect.
  test('the app sets the system proxy while connected without TUN', () async {
    final fake = _FakeSysProxy();
    systemProxyRunner = fake.run;
    systemProxyJournal = () => fake.journal;
    addTearDown(() {
      systemProxyRunner = null;
      systemProxyJournal = null;
    });
    final state = AppState(DemoBackend());
    addTearDown(state.dispose);
    state.start();
    for (var i = 0; i < 50 && !state.loaded; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    await state.updateSettings((s) {
      s['tun'] = false;
      (s['proxy'] as Map)['system'] = true;
    });
    await state.connect();
    for (var i = 0; i < 100 && fake.calls.isEmpty; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    Iterable<String> proxyLines() => state.logs.where((l) => l.source == 'прокси').map((l) => l.message);
    for (var i = 0; i < 100 && !proxyLines().any((l) => l.startsWith('системный')); i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    expect(state.status.state, ConnState.connected);
    expect(fake.calls.single, ['apply', '-addr', '127.0.0.1:17890']);
    expect(proxyLines(), contains(startsWith('системный прокси включён')));
    // The engine's own line: the port is open for programs.
    expect(proxyLines(), contains(startsWith('прокси SOCKS5 и HTTP на 127.0.0.1:17890 открыт')));
    await state.disconnect();
    for (var i = 0; i < 100 && fake.calls.length < 2; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    for (var i = 0; i < 100 && !proxyLines().any((l) => l.contains('восстановлены')); i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    expect(fake.calls.last, ['restore']);
    expect(proxyLines(), contains('системный прокси выключен, прежние настройки восстановлены (Windows)'));
  });

  test('the proxy credentials never go into the journal', () {
    final lines = settingsChanges(
      {
        'proxy': {'user': 'csold', 'pass': 'OldSecret', 'open_http': false},
      },
      {
        'proxy': {'user': 'csnew', 'pass': 'NewSecret', 'open_http': true},
      },
    );
    expect(lines, containsAll(['proxy.user: изменён', 'proxy.pass: изменён', 'proxy.open_http: нет → да']));
    expect(lines.join(), isNot(contains('Secret')));
    expect(lines.join(), isNot(contains('csnew')));
  });
}
