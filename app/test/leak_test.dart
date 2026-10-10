import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/state/leak.dart';
import 'package:coreshift/ui/pages/leak_check.dart';

// The engine's LeakResult, as POST /v1/leaktest returns it. The verdict and
// the roles of the DNS servers are the engine's own
// (engine/internal/service/leakjudge.go, tested there).
Json result({
  Json? exit,
  List<Json> dns = const [],
  Json? home = const {'ip': '198.51.100.37', 'country': 'RU'},
  String serverIp = '203.0.113.5',
  bool checked = true,
  bool fakeIp = false,
  String verdict = 'ok',
  String provider = '',
}) => {
  'server': 'Германия',
  'server_ip': serverIp,
  'exit': ?exit,
  'dns': dns,
  'system': {'checked': checked, 'fake_ip': fakeIp},
  'home': ?home,
  'verdict': verdict,
  if (provider.isNotEmpty) 'provider': provider,
};

Json host(String ip, String cc, String org, [String path = '', String role = '']) => {
  'ip': ip,
  'country': cc,
  'country_name': {'de': 'Germany', 'ru': 'Russian Federation', 'nl': 'Netherlands', 'se': 'Sweden', 'ua': 'Ukraine'}[cc] ?? cc,
  'org': org,
  'asn': 64500,
  'path': path,
  if (role.isNotEmpty) 'role': role,
};

final vpnExit = host('203.0.113.5', 'de', 'Example Hosting');
final serverDNS = host('203.0.113.53', 'de', 'Example Hosting', 'proxy', 'vpn');
final ispDNS = [for (var i = 115; i <= 119; i++) host('198.51.100.$i', 'ru', 'Example ISP LLC', 'system', 'isp')];

/// The user's report: bash.ws saw the ISP's address and the ISP's DNS.
final userCase = result(
  exit: host('198.51.100.37', 'ru', 'Example Telecom LLC'),
  dns: [for (var i = 115; i <= 119; i++) host('198.51.100.$i', 'ru', 'Example ISP LLC', 'system')],
  verdict: 'bypass',
);

/// The tester's report: AdGuard set for the tunnel, answering from Germany
/// (DataCamp), Cloudflare's resolvers behind the VPN server, the exit in
/// Ukraine as bash.ws knows it. Not a leak.
final ownCase = result(
  exit: host('203.0.113.6', 'ua', 'Example Hosting'),
  dns: [
    host('94.140.14.1', 'de', 'DataCamp Limited', 'system', 'own'),
    host('94.140.14.2', 'de', 'DataCamp Limited', 'system', 'own'),
    host('104.23.222.72', 'ru', 'CloudFlare Inc', 'proxy', 'vpn'),
  ],
  serverIp: '203.0.113.6',
  provider: 'AdGuard',
);

LeakReport report(Json j) => LeakReport.fromJson(j);

void main() {
  group('reading the engine\'s verdict', () {
    test('through the VPN with fake IP: no leak', () {
      final r = report(result(exit: vpnExit, dns: [serverDNS], fakeIp: true));
      expect(r.verdict, LeakVerdict.ok);
      expect(r.suspicious, isEmpty);
      expect(r.ownDns, isEmpty);
      expect(r.fakeIp, isTrue);
    });

    test('the user\'s own DNS abroad is no leak', () {
      final r = report(ownCase);
      expect(r.verdict, LeakVerdict.ok);
      expect(r.provider, 'AdGuard');
      expect(r.ownDns.map((d) => d.ip), ['94.140.14.1', '94.140.14.2']);
      expect(r.suspicious, isEmpty);
      expect(r.leaks, isFalse);
    });

    test('the test went around the VPN: no verdict', () {
      final r = report(userCase);
      expect(r.bypassed, isTrue);
      expect(r.verdict, LeakVerdict.bypass);
      expect(r.leaks, isFalse);
    });

    test('the ISP\'s resolvers answer apps: a leak', () {
      final r = report(result(exit: vpnExit, dns: [...ispDNS, serverDNS], verdict: 'leak'));
      expect(r.verdict, LeakVerdict.leak);
      expect(r.leaks, isTrue);
      expect(r.suspicious.map((d) => d.ip), [for (final d in ispDNS) d['ip']]);
    });

    test('a verdict the app does not know, or none: unknown', () {
      expect(report({...result(exit: vpnExit), 'verdict': 'new'}).verdict, LeakVerdict.unknown);
      expect(report(result(exit: vpnExit)..remove('verdict')).verdict, LeakVerdict.unknown);
      expect(report(result(verdict: 'unknown')).verdict, LeakVerdict.unknown);
    });

    test('reads the engine JSON', () {
      final r = report(result(exit: vpnExit, dns: [serverDNS], fakeIp: true));
      expect(r.exit?.country, 'de');
      expect(r.exit?.place, 'Germany, Example Hosting');
      expect(r.dns.single.path, 'proxy');
      expect(r.dns.single.role, 'vpn');
      expect(r.home?.ip, '198.51.100.37');
      expect(r.serverIp, '203.0.113.5');
      expect(r.server, 'Германия');
      expect(r.systemChecked, isTrue);
    });

    test('the ASN number does not get in the way of the organisation', () {
      // The engine sends the number as "asn"; with no organisation the
      // server is known by what bash.ws gave as text, if anything.
      final s = LeakServer.fromJson({'ip': '1.2.3.4', 'country': 'DE', 'asn': 64500});
      expect(s.org, '');
      expect(LeakServer.fromJson({'ip': '1.2.3.4', 'asn': 'AS64500 Hosting'}).org, 'AS64500 Hosting');
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
        // Around the VPN the address is the device's, not the server's.
        expect(find.text('Сайты видят вас из'), findsOneWidget);
        expect(find.text('Адрес VPN-сервера для сайтов'), findsNothing);
        expect(find.byIcon(Icons.check_circle_outline), findsNothing);
        await finish(tester, state);
      });

      testWidgets('$name: a leak', (tester) async {
        final state = await showResult(tester, result(exit: vpnExit, dns: [...ispDNS, serverDNS], verdict: 'leak'));
        expect(find.text('Есть утечка DNS'), findsOneWidget);
        expect(find.textContaining('DNS вашего провайдера'), findsOneWidget);
        expect(find.textContaining('Что сделать'), findsOneWidget);
        expect(find.text('DNS, которым пользуются приложения (через VPN)'), findsOneWidget);
        expect(find.text('DNS на стороне VPN-сервера'), findsOneWidget);
        // A warning for each of the ISP's servers (and the title's), a tick
        // for the VPN server's.
        expect(find.byIcon(Icons.warning_amber_rounded), findsNWidgets(ispDNS.length + 1));
        expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
        await finish(tester, state);
      });

      testWidgets('$name: no leak', (tester) async {
        final state = await showResult(tester, result(exit: vpnExit, dns: [serverDNS], fakeIp: true));
        expect(find.text('Утечки нет'), findsOneWidget);
        expect(find.textContaining('выдаёт сам туннель'), findsOneWidget);
        expect(find.text('Отвечает сам туннель, наружу запросы не уходят'), findsOneWidget);
        expect(find.text('Адрес VPN-сервера для сайтов'), findsOneWidget);
        expect(find.byIcon(Icons.warning_amber_rounded), findsNothing);
        await finish(tester, state);
      });

      testWidgets('$name: the user\'s own DNS in another country', (tester) async {
        final state = await showResult(tester, ownCase);
        expect(find.text('Утечки нет'), findsOneWidget);
        expect(find.text('Есть утечка DNS'), findsNothing);
        expect(find.textContaining('DNS идёт через VPN к выбранному вами DNS (AdGuard, Германия)'), findsOneWidget);
        expect(find.text('Адрес VPN-сервера для сайтов'), findsOneWidget);
        expect(find.text('Сайты видят вас из'), findsNothing);
        expect(find.text('DNS, которым пользуются приложения (через VPN) — AdGuard'), findsOneWidget);
        expect(find.text('DNS на стороне VPN-сервера'), findsOneWidget);
        expect(find.textContaining('94.140.14.1'), findsOneWidget);
        expect(find.textContaining('DataCamp Limited'), findsNWidgets(2));
        expect(find.textContaining('· ваш DNS'), findsNWidgets(2));
        expect(find.textContaining('Страна — по базе сервиса проверки'), findsOneWidget);
        // The user's DNS and the VPN's side are ticked, nothing is warned of.
        expect(find.byIcon(Icons.check_circle_outline), findsNWidgets(3));
        expect(find.byIcon(Icons.warning_amber_rounded), findsNothing);
        await finish(tester, state);
      });

      testWidgets('$name: a server nobody knows stays unmarked', (tester) async {
        final state = await showResult(tester, result(exit: vpnExit, dns: [host('192.0.2.9', 'nl', 'Other Hosting', 'system')]));
        expect(find.text('Утечки нет'), findsOneWidget);
        expect(find.byIcon(Icons.warning_amber_rounded), findsNothing);
        expect(find.byIcon(Icons.check_circle_outline), findsNothing);
        expect(find.textContaining('192.0.2.9'), findsOneWidget);
        await finish(tester, state);
      });
    }
  });
}
