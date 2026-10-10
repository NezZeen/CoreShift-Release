import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/backend.dart';
import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';

/// The demo service; with [serverDown] its checkup finds the server dead,
/// as the engine says it, events and all.
class _Backend extends DemoBackend {
  final calls = <(String, String, Object?)>[];
  bool serverDown = false;

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    calls.add((method, path, body));
    if (serverDown && method == 'POST' && path == '/v1/checkup') {
      await Future.delayed(const Duration(milliseconds: 300));
      return {
        'state': 'connected',
        'server': 'Amsterdam',
        'steps': [
          {'id': 'network', 'title': 'Сеть устройства', 'status': 'ok', 'detail': 'подключено'},
          {'id': 'internet', 'title': 'Интернет напрямую', 'status': 'ok', 'detail': 'отвечают Cloudflare, Google, Яндекс (21 мс)'},
          {'id': 'server', 'title': 'Сервер', 'status': 'fail', 'detail': '«Amsterdam»: порт сервера не отвечает (нет ответа)'},
          {'id': 'tunnel', 'title': 'Связь через VPN', 'status': 'fail', 'detail': 'запросы через сервер не проходят: нет ответа'},
        ],
        'verdict': {
          'cause': 'server-down',
          'status': 'fail',
          'title': 'Сервер «Amsterdam» не отвечает — выберите другой',
          'advice': 'Интернет работает, а сервер нет: он выключен или заблокирован.',
          'actions': ['servers', 'reconnect'],
        },
      };
    }
    return super.call(method, path, body);
  }
}

void main() {
  Future<(AppState, _Backend, List<(String, String)>)> pumpApp(WidgetTester tester, Size size, {bool connect = true}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final backend = _Backend();
    final saved = <(String, String)>[];
    final state = AppState(
      backend,
      journalSaver: (name, text) async {
        saved.add((name, text));
        return 'C:\\Users\\me\\Downloads\\$name';
      },
    );
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    if (connect) {
      await tester.runAsync(() async {
        await state.connect();
        await Future.delayed(const Duration(milliseconds: 2300));
      });
      await tester.pump();
      expect(state.status.state, ConnState.connected);
      backend.quiet();
    }
    return (state, backend, saved);
  }

  /// Moves the test's clock on, through the demo service's delays.
  Future<void> advance(WidgetTester tester, [int steps = 40]) async {
    for (var i = 0; i < steps; i++) {
      await tester.pump(const Duration(milliseconds: 100));
    }
  }

  /// Lets a window or a sheet open or close: a frame to start the
  /// animation, its length, a frame to drop the route.
  Future<void> settle(WidgetTester tester) async {
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 600));
    await tester.pump();
  }

  /// The home page's button of the checkup's line.
  Finder checkupButton() => find.descendant(
    of: find.ancestor(of: find.text('Проверить всё'), matching: find.byType(Row)).first,
    matching: find.text('Запустить'),
  );

  testWidgets('from the home page the steps tick off, the verdict comes, and the report goes to support', (tester) async {
    for (final size in [const Size(390, 844), const Size(1400, 900)]) {
      final (state, backend, saved) = await pumpApp(tester, size);
      await tester.ensureVisible(checkupButton());
      await tester.tap(checkupButton());
      await tester.pump();
      expect(state.checkup.running, isTrue, reason: 'running at $size');
      // Every step is listed at once, a spinner beside those under way.
      expect(find.text('Связь через VPN'), findsOneWidget);
      expect(find.byType(CircularProgressIndicator), findsWidgets);
      final asked = backend.calls.where((c) => c.$1 == 'POST' && c.$2 == '/v1/checkup').toList();
      expect(asked, hasLength(1));
      expect((asked.single.$3 as Map)['speed'], isTrue);

      // Halfway the first steps are done, before the whole result.
      await advance(tester, 8);
      expect(state.checkup.running, isTrue);
      expect(state.checkup.steps.where((s) => s.status == 'ok'), isNotEmpty, reason: 'steps from the events at $size');

      await advance(tester);
      expect(state.checkup.running, isFalse);
      expect(find.text('Всё работает'), findsOneWidget, reason: 'verdict at $size');
      expect(find.text('работает, задержка 168 мс'), findsOneWidget);
      expect(find.byIcon(Icons.check_circle), findsWidgets);
      expect(tester.takeException(), isNull);

      // A few lines of the journal, from «диагностика».
      final lines = state.logs.where((l) => l.source == 'диагностика').toList();
      expect(lines.first.message, 'итог: Всё работает');
      expect(lines.first.level, LogLevel.ok);
      expect(lines[1].message, contains('Связь через VPN ✓'));

      await tester.ensureVisible(find.text('Отправить в поддержку'));
      await tester.tap(find.text('Отправить в поддержку'));
      await tester.pump();
      await tester.pump();
      expect(saved, hasLength(1));
      final (name, text) = saved.single;
      expect(name, startsWith('CoreShift-'));
      expect(name, contains('-check-'));
      expect(text, startsWith('Проверка CoreShift: '));
      expect(text, contains('Итог: Всё работает'));
      expect(text, contains('✓ Связь через VPN — работает, задержка 168 мс'));
      // The journal follows, with its header.
      expect(text.indexOf('CoreShift: приложение'), greaterThan(text.indexOf('Итог:')));
      // No subscription links nor their tokens.
      expect(text, isNot(contains('TOKEN')));
      expect(text, isNot(contains('sub.northlink.example')));
      expect(find.textContaining('сохранён в «Загрузки»'), findsOneWidget);
      expect(find.text('Показать'), findsOneWidget);

      // Closed, the home page says what it found.
      if (size.width > 700) {
        await tester.tap(find.text('Закрыть'));
      } else {
        Navigator.of(tester.element(find.text('Отправить в поддержку'))).pop();
      }
      await settle(tester);
      expect(find.text('Отправить в поддержку'), findsNothing);
      expect(find.textContaining('Всё работает'), findsOneWidget);
      expect(find.text('Ещё раз'), findsWidgets);

      await tester.runAsync(() => state.disconnect());
      await tester.pump(const Duration(seconds: 1));
    }
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('a dead server: the verdict says so and «Другой сервер» opens the pick', (tester) async {
    for (final size in [const Size(390, 844), const Size(1400, 900)]) {
      final (state, backend, _) = await pumpApp(tester, size);
      backend.serverDown = true;
      await tester.ensureVisible(checkupButton());
      await tester.tap(checkupButton());
      await advance(tester, 10);
      expect(find.text('Сервер «Amsterdam» не отвечает — выберите другой'), findsOneWidget, reason: 'at $size');
      expect(find.byIcon(Icons.cancel), findsWidgets);
      final errs = state.logs.where((l) => l.source == 'диагностика' && l.level == LogLevel.err).map((l) => l.message).toList();
      expect(errs, contains('итог: Сервер «Amsterdam» не отвечает — выберите другой'));
      expect(errs, contains('Сервер: «Amsterdam»: порт сервера не отвечает (нет ответа)'));
      expect(find.text('Переподключить'), findsWidgets);

      await tester.tap(find.text('Другой сервер').last);
      await settle(tester);
      expect(find.text('Выбор сервера'), findsOneWidget, reason: 'the quick pick at $size');
      expect(find.text('Отправить в поддержку'), findsNothing, reason: 'the checkup closed first at $size');
      expect(tester.takeException(), isNull);
      Navigator.of(tester.element(find.text('Выбор сервера'))).pop();
      await settle(tester);

      await tester.runAsync(() => state.disconnect());
      await tester.pump(const Duration(seconds: 1));
    }
    await tester.pump(const Duration(seconds: 6));
  });

  testWidgets('in the settings, beside the leak test, also without a connection', (tester) async {
    for (final size in [const Size(390, 844), const Size(1400, 900)]) {
      final (state, _, _) = await pumpApp(tester, size, connect: false);
      final phone = find.byType(NavigationBar).evaluate().isNotEmpty;
      await tester.tap(phone ? find.descendant(of: find.byType(NavigationBar), matching: find.text('Настройки')) : find.text('Настройки').first);
      await tester.pump();
      final button = find.widgetWithText(InkWell, 'Проверить всё').last;
      await tester.ensureVisible(button);
      await tester.tap(button);
      await advance(tester);
      expect(state.checkup.done, isTrue, reason: 'at $size');
      expect(find.text('Сеть в порядке, VPN не подключён'), findsOneWidget);
      // Not connected: the tunnel's steps are left out, and «Подключить» connects.
      expect(find.text('VPN не подключён'), findsWidgets);
      expect(find.text('Подключить'), findsWidgets);
      expect(tester.takeException(), isNull);
      await tester.tap(find.text('Подключить').last);
      await advance(tester, 25);
      expect(state.status.active, isTrue);
      await tester.runAsync(() => state.disconnect());
      await tester.pump(const Duration(seconds: 1));
    }
    await tester.pump(const Duration(seconds: 6));
  });

  test('an older service without the checkup says so', () async {
    final state = AppState(_OldBackend());
    state.start();
    for (var i = 0; i < 50 && !state.loaded; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    await state.runCheckup();
    expect(state.checkup.done, isFalse);
    expect(state.checkup.error, contains('старой версии'));
    expect(state.checkupReport().join('\n'), contains('Итог: Служба CoreShift старой версии'));
  });
}

class _OldBackend extends DemoBackend {
  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    if (path == '/v1/checkup') throw const ApiError(404, '404 page not found');
    return super.call(method, path, body);
  }
}
