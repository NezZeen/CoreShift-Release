import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/state/leak.dart';
import 'package:coreshift/ui/pages/leak_check.dart';

// The engine's LeakResult, as POST /v1/leaktest returns it.
Json result({
  Json? exit,
  List<Json> dns = const [],
  Json? home = const {'ip': '198.51.100.37', 'country': 'RU'},
  String serverIp = '203.0.113.5',
  bool checked = true,
  bool fakeIp = false,
}) => {
  'server': 'Германия',
  'server_ip': serverIp,
  'exit': ?exit,
  'dns': dns,
  'system': {'checked': checked, 'fake_ip': fakeIp},
  'home': ?home,
};

Json host(String ip, String cc, String org, [String path = '']) => {
  'ip': ip,
  'country': cc,
  'country_name': {'de': 'Germany', 'ru': 'Russian Federation', 'nl': 'Netherlands', 'se': 'Sweden'}[cc] ?? cc,
  'org': org,
  'path': path,
};

final vpnExit = host('203.0.113.5', 'de', 'Example Hosting');
final serverDNS = host('203.0.113.53', 'de', 'Example Hosting', 'proxy');
final ispDNS = [for (var i = 115; i <= 119; i++) host('198.51.100.$i', 'ru', 'Example ISP LLC', 'system')];

/// The user's report: bash.ws saw the ISP's address and the ISP's DNS.
final userCase = result(exit: host('198.51.100.37', 'ru', 'Example Telecom LLC'), dns: ispDNS);

LeakReport report(Json j) => LeakReport.fromJson(j);

void main() {
  group('verdict', () {
    test('through the VPN with fake IP: no leak', () {
      final r = report(result(exit: vpnExit, dns: [serverDNS], fakeIp: true));
      expect(r.verdict, LeakVerdict.ok);
      expect(r.suspicious, isEmpty);
      expect(r.fakeIp, isTrue);
    });

    test('apps resolved by a public resolver through the tunnel: no leak', () {
      final r = report(result(exit: vpnExit, dns: [host('104.23.222.72', 'se', 'CloudFlare Inc', 'system'), serverDNS]));
      expect(r.verdict, LeakVerdict.ok);
    });

    test('the test went around the VPN: no verdict', () {
      final r = report(userCase);
      expect(r.bypassed, isTrue);
      expect(r.verdict, LeakVerdict.bypass);
      expect(r.leaks, isFalse);
    });

    test('a neighbouring address of the ISP counts as going around', () {
      final r = report(result(exit: host('198.51.100.40', 'ru', 'Example Telecom LLC'), dns: [serverDNS]));
      expect(r.verdict, LeakVerdict.bypass);
    });

    test("the ISP's resolvers answer apps: a leak", () {
      final r = report(result(exit: vpnExit, dns: [...ispDNS, serverDNS]));
      expect(r.verdict, LeakVerdict.leak);
      expect(r.ispResolvers, isTrue);
      expect(r.suspicious.map((d) => d.ip), [for (final d in ispDNS) d['ip']]);
    });

    test("a resolver outside the server's country answers apps: a leak", () {
      final r = report(result(exit: vpnExit, dns: [host('198.51.100.53', 'nl', 'Some Telecom', 'system')]));
      expect(r.verdict, LeakVerdict.leak);
      expect(r.ispResolvers, isFalse);
    });

    test("the server's own resolver abroad is on the VPN's side", () {
      final r = report(result(exit: vpnExit, dns: [host('198.51.100.53', 'nl', 'Other Hosting', 'proxy')]));
      expect(r.verdict, LeakVerdict.ok);
    });

    test('a resolver in the home country is suspect on any path', () {
      final r = report(result(exit: vpnExit, dns: [host('77.88.8.8', 'ru', 'YANDEX LLC', 'proxy')]));
      expect(r.verdict, LeakVerdict.leak);
    });

    test('without the home address only the server address vouches for the exit', () {
      expect(report(result(exit: vpnExit, dns: [serverDNS], home: null)).verdict, LeakVerdict.ok);
      final relayed = report(result(exit: host('198.51.100.9', 'de', 'CDN'), dns: [serverDNS], home: null));
      expect(relayed.verdict, LeakVerdict.unknown);
    });

    test('no exit address: unknown', () {
      expect(report(result(dns: [serverDNS])).verdict, LeakVerdict.unknown);
    });

    test('reads the engine JSON', () {
      final r = report(result(exit: vpnExit, dns: [serverDNS], fakeIp: true));
      expect(r.exit?.country, 'de');
      expect(r.exit?.place, 'Germany, Example Hosting');
      expect(r.dns.single.path, 'proxy');
      expect(r.home?.ip, '198.51.100.37');
      expect(r.serverIp, '203.0.113.5');
      expect(r.server, 'Германия');
      expect(r.systemChecked, isTrue);
    });
  });

  group('result card', () {
    for (final (name, size) in [('phone', const Size(390, 844)), ('desktop', const Size(1400, 900))]) {
      Future<AppState> showResult(WidgetTester tester, Json json) async {
        tester.view.physicalSize = size;
        tester.view.devicePixelRatio = 1;
        addTearDown(tester.view.reset);
        final state = AppState(DemoBackend(), leakTestRunner: () async => json);
        await tester.runAsync(() async {
          state.start();
          for (var i = 0; i < 50 && !state.loaded; i++) {
            await Future.delayed(const Duration(milliseconds: 20));
          }
          await state.connect();
          await state.runLeakTest();
        });
        await tester.pumpWidget(CoreShiftApp(state: state));
        await tester.pump();
        final phone = find.byType(NavigationBar).evaluate().isNotEmpty;
        expect(phone, name == 'phone');
        await tester.tap(phone ? find.descendant(of: find.byType(NavigationBar), matching: find.text('Настройки')) : find.text('Настройки').first);
        await tester.pump();
        await tester.ensureVisible(find.byType(LeakResultCard));
        await tester.pump();
        return state;
      }

      Future<void> finish(WidgetTester tester, AppState state) async {
        expect(tester.takeException(), isNull);
        await tester.runAsync(() => state.disconnect());
        await tester.pump(const Duration(seconds: 1));
        await tester.pump(const Duration(seconds: 1));
      }

      testWidgets('$name: around the VPN', (tester) async {
        final state = await showResult(tester, userCase);
        expect(find.text('Проверка прошла мимо VPN'), findsOneWidget);
        expect(find.text('Утечки нет'), findsNothing);
        expect(find.textContaining('результат недостоверен'), findsOneWidget);
        expect(find.text('Ваш адрес без VPN'), findsOneWidget);
        expect(find.textContaining('198.51.100.119'), findsOneWidget);
        await finish(tester, state);
      });

      testWidgets('$name: a leak', (tester) async {
        final state = await showResult(tester, result(exit: vpnExit, dns: [...ispDNS, serverDNS]));
        expect(find.text('Есть утечка DNS'), findsOneWidget);
        expect(find.textContaining('DNS вашего провайдера'), findsOneWidget);
        expect(find.textContaining('Что сделать'), findsOneWidget);
        expect(find.text('DNS приложений'), findsOneWidget);
        expect(find.text('DNS на стороне VPN-сервера'), findsOneWidget);
        expect(find.byIcon(Icons.warning_amber_rounded), findsNWidgets(ispDNS.length + 1));
        await finish(tester, state);
      });

      testWidgets('$name: no leak', (tester) async {
        final state = await showResult(tester, result(exit: vpnExit, dns: [serverDNS], fakeIp: true));
        expect(find.text('Утечки нет'), findsOneWidget);
        expect(find.textContaining('выдаёт сам туннель'), findsOneWidget);
        expect(find.text('Отвечает сам туннель, наружу запросы не уходят'), findsOneWidget);
        await finish(tester, state);
      });
    }
  });
}
