import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/rules.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/state/errors.dart';

void main() {
  test('rules are tidied as the service stores them', () {
    const good = {
      ' GeoSite:Category-Ads-All ': 'geosite:category-ads-all',
      'geosite:google@cn': 'geosite:google@cn',
      'geosite:geolocation-!cn': 'geosite:geolocation-!cn',
      'geoip:RU': 'geoip:ru',
      'domain:Example.COM': 'example.com',
      '.bank.example.': 'bank.example',
      'Госуслуги.рф': 'xn--c1aapkosapc.xn--p1ai',
      '10.8.0.0/16': '10.8.0.0/16',
      '203.0.113.7': '203.0.113.7',
      '2001:db8::/32': '2001:db8::/32',
    };
    good.forEach((raw, want) {
      final r = parseRule(raw);
      expect(r.error, isNull, reason: raw);
      expect(r.match, want, reason: raw);
    });
    const bad = {
      '': 'Введите правило',
      'geosite:': 'Укажите категорию',
      'geosite:../etc': 'только латинские',
      'geosite:ads all': 'только латинские',
      'geosite:-ads': 'только латинские',
      'geosite:google@': 'Пустая категория',
      'geoip:ru@cn': 'нет атрибутов',
      'geosit:youtube': 'Неизвестное начало',
      'bad domain': 'не похоже на адрес сайта',
      '300.1.1.1': 'не похоже на IP-адрес',
      '10.0.0.0/40': 'не похоже на IP-адрес',
    };
    bad.forEach((raw, want) => expect(parseRule(raw).error, contains(want), reason: raw));
    expect(parseRule('geosite:${'a' * 65}').error, contains('длиннее'));
    expect(ruleLabel('xn--c1aapkosapc.xn--p1ai'), 'госуслуги.рф');
    expect(ruleLabel('geosite:youtube'), 'geosite:youtube');
    expect(sourceLinkError('http://example.org/{name}.srs'), contains('https'));
    expect(sourceLinkError('https://example.org/{name}/{name}.srs'), contains('один раз'));
    expect(sourceLinkError('https://example.org/geosite.dat'), isNull);
  });

  test("the service's errors about rules are said in Russian", () {
    expect(humanError('routing.rules: "geosite:a b": category name may have only a-z, 0-9 and !@._-'), contains('только латинские'));
    expect(humanError('routing.rules: "x.example": "allow" is not an action (proxy, direct or block)'), contains('неизвестное действие'));
    expect(humanError('routing.rules: at most 64 categories'), contains('не больше 64'));
    expect(humanError('routing.geo.geosite_url: link must start with https://'), contains('https://'));
  });

  test('rules and the source round-trip through the settings', () async {
    final b = DemoBackend();
    final set = await b.call('GET', '/v1/settings') as Map;
    expect(set['routing']['block_ads'], true);
    expect(set['routing']['russia_abroad'], false);
    expect(set['routing']['geo']['source'], 'sagernet');
    set['routing']['rules'] = [
      {'match': 'GeoSite:YouTube', 'action': 'proxy'},
      {'match': 'geosite:youtube', 'action': 'direct'},
      {'match': 'geoip:ru', 'action': 'direct'},
    ];
    set['routing']['geo'] = {'source': 'custom', 'geosite_url': 'https://example.org/geosite.dat', 'geoip_url': '', 'presets': true};
    final saved = await b.call('PUT', '/v1/settings', set) as Map;
    expect(saved['routing']['rules'], [
      {'match': 'geosite:youtube', 'action': 'proxy'},
      {'match': 'geoip:ru', 'action': 'direct'},
    ]);
    final again = await b.call('GET', '/v1/settings') as Map;
    expect(again['routing']['geo']['geosite_url'], 'https://example.org/geosite.dat');
    expect(again['routing']['geo']['presets'], true);

    set['routing']['rules'] = [
      {'match': 'geosite:a b', 'action': 'block'},
    ];
    await expectLater(b.call('PUT', '/v1/settings', set), throwsA(anything));
    set['routing']['rules'] = <Map>[];
    set['routing']['geo'] = {'source': 'custom', 'geosite_url': 'http://example.org/geosite.dat', 'geoip_url': '', 'presets': false};
    await expectLater(b.call('PUT', '/v1/settings', set), throwsA(anything));
  });

  Future<AppState> pumpRules(WidgetTester tester, Size size) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(DemoBackend());
    await tester.runAsync(() async {
      state.start();
      for (var i = 0; i < 50 && !state.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pumpWidget(CoreShiftApp(state: state));
    await tester.pump();
    final phone = find.byType(NavigationBar).evaluate().isNotEmpty;
    await tester.tap(phone ? find.descendant(of: find.byType(NavigationBar), matching: find.text('Правила')) : find.text('Правила').first);
    await tester.pump();
    return state;
  }

  for (final (name, size) in [('desktop', Size(1400, 900)), ('phone', Size(390, 844))]) {
    testWidgets('own rules: add, reorder, remove ($name)', (tester) async {
      final state = await pumpRules(tester, size);
      List<String> rules() => [for (final r in state.setting<List>('routing.rules', const [])) '${r['match']} ${r['action']}'];
      expect(find.text('Свои правила'), findsOneWidget);
      expect(find.text('Блокировать рекламу'), findsOneWidget);
      final field = find.widgetWithText(TextField, 'geosite:youtube, geoip:ru, example.com');

      Future<void> add(String text, String action) async {
        await tester.ensureVisible(field);
        await tester.enterText(field, text);
        final seg = find.ancestor(of: field, matching: find.byType(Column)).first;
        final pill = find.descendant(of: seg, matching: find.text(action));
        await tester.ensureVisible(pill.first);
        await tester.tap(pill.first);
        await tester.pump();
        await tester.ensureVisible(field);
        await tester.tap(find.text('Добавить').last);
        await tester.pump(const Duration(milliseconds: 300));
        await tester.pump();
      }

      await add('geosite:category-ads-all', 'Блокировать');
      await add('GeoSite:YouTube', 'Через VPN');
      await add('geoip:ru', 'Напрямую');
      expect(rules(), ['geosite:category-ads-all block', 'geosite:youtube proxy', 'geoip:ru direct']);
      expect(find.text('geosite:youtube'), findsOneWidget);

      // A mistake is explained before anything is sent.
      await add('geosite:ads all', 'Блокировать');
      expect(find.textContaining('только латинские'), findsOneWidget);
      expect(rules().length, 3);

      // Up, and away.
      await tester.ensureVisible(find.text('geoip:ru'));
      final row = find.ancestor(of: find.text('geoip:ru'), matching: find.byKey(const ValueKey('rule-geoip:ru')));
      await tester.tap(find.descendant(of: row, matching: find.byIcon(Icons.arrow_upward)));
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump();
      expect(rules(), ['geosite:category-ads-all block', 'geoip:ru direct', 'geosite:youtube proxy']);
      final ads = find.byKey(const ValueKey('rule-geosite:category-ads-all'));
      await tester.ensureVisible(ads);
      await tester.tap(find.descendant(of: ads, matching: find.byIcon(Icons.close)));
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump();
      expect(rules(), ['geoip:ru direct', 'geosite:youtube proxy']);

      // Another source, with a link of the user's own: once the toasts,
      // which a phone shows over the bottom of the page, are gone.
      await tester.pump(const Duration(seconds: 6));
      await tester.ensureVisible(find.text('Источник баз'));
      await tester.tap(find.text('Источник баз'));
      await tester.pump();
      await tester.ensureVisible(find.text('Своя ссылка'));
      await tester.tap(find.text('Своя ссылка'));
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump();
      expect(state.setting('routing.geo.source', ''), 'custom');
      expect(find.textContaining('сторонний источник'), findsOneWidget);
      final link = find.widgetWithText(TextField, 'https://example.org/geosite.dat');
      await tester.ensureVisible(link);
      await tester.enterText(link, 'https://lists.example/geosite.dat');
      await tester.testTextInput.receiveAction(TextInputAction.done);
      await tester.pump(const Duration(milliseconds: 300));
      await tester.pump();
      expect(state.setting('routing.geo.geosite_url', ''), 'https://lists.example/geosite.dat');
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 6));
    });
  }
}
