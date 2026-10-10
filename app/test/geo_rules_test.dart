import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/demo_geo.dart';
import 'package:coreshift/api/geo.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/widgets.dart';

void main() {
  test('names are the same whatever their spelling', () {
    for (final s in ['ua', 'UA', 'U_a', 'u.a', 'u a', 'category:ua', 'Category-UA', 'geosite:category-ua']) {
      expect(normalizeCategory(s), 'ua', reason: s);
    }
    expect(normalizeCategory('category-media-ru-blocked'), 'mediarublocked');
    expect(normalizeCategory('google@cn'), 'google@cn');
    expect(normalizeCategory('category'), 'category');

    const names = ['category-ru', 'category-ua', 'ua', 'youtube', 'google'];
    expect(resolveCategory(names, 'youtube').exact, isTrue);
    expect(resolveCategory(names, 'YouTube').name, 'youtube');
    expect(resolveCategory(names, 'ru').name, 'category-ru');
    expect(resolveCategory(names, 'ua').name, 'ua', reason: 'the exact name wins');
    expect(resolveCategory(names, 'U_a').ambiguous, ['category-ua', 'ua']);
    expect(resolveCategory(names, 'U_a').name, isEmpty);
    expect(resolveCategory(names, 'google@cn').name, 'google@cn');
    final typo = resolveCategory(names, 'Uua');
    expect(typo.name, isEmpty, reason: 'a typo is never used');
    expect(typo.suggestions, ['category-ua', 'ua']);
    expect(resolveCategory(names, 'zzzzzz').suggestions, isEmpty);
    expect(completeCategory(names, 'ca'), ['category-ru', 'category-ua']);
  });

  test('the state of a base in words', () {
    final now = DateTime(2026, 10, 10, 15, 0);
    GeoBase base(String state, {int percent = -1, DateTime? at, String error = ''}) => GeoBase(state: state, percent: percent, loadedAt: at, error: error);
    expect(baseStateText(base(baseLoading, percent: 42), now), 'загружается… 42%');
    expect(baseStateText(base(baseLoading), now), 'загружается…');
    expect(baseStateText(base(baseLoaded, at: DateTime(2026, 10, 10, 14, 5)), now), 'загружена в 14:05');
    expect(baseStateText(base(baseLoaded, at: DateTime(2026, 10, 9, 9, 7)), now), 'загружена в 09.10 в 09:07');
    expect(baseStateText(base(baseError, error: 'no such host'), now), 'ошибка: no such host');
    expect(baseStateText(base(baseIdle), now), 'ещё не загружена');
    expect(inBase('runetfreedom'), 'в базе runetfreedom');
    expect(inBase('custom'), 'в базе своей ссылки');
  });

  test('the demo answers about names and bases', () async {
    final b = DemoBackend();
    Future<GeoCategories> ask(String kind, String q) async =>
        GeoCategories.fromJson(await b.call('GET', '/v1/geo/categories?kind=$kind&q=${Uri.encodeQueryComponent(q)}') as Map<String, dynamic>);
    final ru = await ask('geosite', 'RU');
    expect(ru.loaded, isTrue);
    expect(ru.resolved, 'category-ru');
    expect(ru.exact, isFalse);
    expect(ru.matches, contains('category-ru'));
    expect((await ask('geosite', 'youtube')).exact, isTrue);
    expect((await ask('geosite', 'tiktoc')).suggestions, ['tiktok']);
    expect((await ask('geoip', 'UA')).resolved, 'ua');
    await expectLater(b.call('GET', '/v1/geo/categories?kind=domain&q=x'), throwsA(anything));

    var st = GeoStatus.fromJson(await b.call('GET', '/v1/geo/status') as Map<String, dynamic>);
    expect([for (final x in st.bases) x.state], [baseLoaded, baseLoaded]);

    // A custom link: loading in steps, then loaded; a link that fails, then Retry.
    final set = await b.call('GET', '/v1/settings') as Map<String, dynamic>;
    set['routing']['geo'] = {'source': 'custom', 'geosite_url': 'https://lists.example/fail.dat', 'geoip_url': '', 'presets': false};
    set['routing']['rules'] = [
      {'match': 'geosite:ru', 'action': 'direct'},
    ];
    await b.call('PUT', '/v1/settings', set);
    final seen = <String>[];
    for (var i = 0; i < 5; i++) {
      st = GeoStatus.fromJson(await b.call('GET', '/v1/geo/status') as Map<String, dynamic>);
      seen.add('${st.base('geosite')!.state}/${st.base('geosite')!.percent}/${st.rule('geosite:ru')?.state}');
    }
    expect(seen, ['loading/-1/waiting', 'loading/-1/waiting', 'loading/-1/waiting', 'error/-1/error', 'error/-1/error']);
    expect(st.base('geosite')!.error, contains('no such host'));
    st = GeoStatus.fromJson(await b.call('POST', '/v1/geo/reload', {'kind': 'geosite'}) as Map<String, dynamic>);
    expect(st.base('geosite')!.state, baseLoading);
    for (var i = 0; i < 4; i++) {
      st = GeoStatus.fromJson(await b.call('GET', '/v1/geo/status') as Map<String, dynamic>);
    }
    expect(st.base('geosite')!.state, baseLoaded);
    expect(st.rule('geosite:ru')!.state, ruleResolved);
    expect(st.rule('geosite:ru')!.name, 'category-ru');
  });

  Future<AppState> pump(WidgetTester tester, Size size) async {
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

  // Time for the demo's answers (120 ms each) and the polls.
  Future<void> wait(WidgetTester tester, [int ms = 400]) async {
    await tester.pump(Duration(milliseconds: ms));
    await tester.pump();
  }

  final fieldFinder = find.widgetWithText(TextField, 'geosite:youtube, geoip:ru, example.com');

  Future<void> type(WidgetTester tester, String text) async {
    await tester.ensureVisible(fieldFinder);
    await tester.enterText(fieldFinder, text);
    await wait(tester, 300); // the pause before asking
    await wait(tester, 300); // the answer
  }

  Future<void> useSource(WidgetTester tester, AppState state, Map<String, Object> geo) async {
    await tester.runAsync(() async {});
    state.updateSettings((x) => x['routing']['geo'] = geo);
    await wait(tester, 400);
  }

  for (final (name, size) in [('desktop', Size(1400, 900)), ('phone', Size(390, 844))]) {
    testWidgets('rule editor: names from the base, a note, a pick ($name)', (tester) async {
      await pump(tester, size);

      // Another spelling of one category: it is said which will be used.
      await type(tester, 'geosite:ru');
      expect(find.byKey(const ValueKey('geo-note-resolved')), findsOneWidget);
      expect(find.descendant(of: find.byKey(const ValueKey('geo-note-resolved')), matching: find.text('category-ru')), findsOneWidget);
      expect(find.textContaining('Будет использована'), findsOneWidget);
      // Autocomplete: the names of the base that fit.
      final matches = find.byKey(const ValueKey('geo-matches'));
      expect(matches, findsOneWidget);
      expect(find.descendant(of: matches, matching: find.text('category-ru')), findsOneWidget);
      expect(find.descendant(of: matches, matching: find.text('category-media-ru-blocked')), findsOneWidget);

      // The name as it is: nothing to guess.
      await type(tester, 'GeoSite:YouTube');
      expect(find.byKey(const ValueKey('geo-note-ok')), findsOneWidget);
      expect(find.textContaining('Есть в базе SagerNet'), findsOneWidget);

      // A typo is not used, only offered; a tap puts the name in.
      await type(tester, 'geosite:Uua');
      expect(find.byKey(const ValueKey('geo-note-typo')), findsOneWidget);
      expect(find.textContaining('Нет в базе. Может быть,'), findsOneWidget);
      final offer = find.descendant(of: find.byKey(const ValueKey('geo-note-typo')), matching: find.text('category-ua'));
      expect(offer, findsOneWidget);
      await tester.tap(offer);
      await wait(tester, 300);
      await wait(tester, 300);
      expect(tester.widget<TextField>(fieldFinder).controller!.text, 'geosite:category-ua');
      expect(find.byKey(const ValueKey('geo-note-ok')), findsOneWidget);

      // Nothing near it.
      await type(tester, 'geosite:qqqqqqqq');
      expect(find.byKey(const ValueKey('geo-note-missing')), findsOneWidget);
      expect(find.textContaining('Нет такой категории в базе SagerNet'), findsOneWidget);

      // Not a category: nothing to check.
      await type(tester, 'example.com');
      expect(find.byKey(const ValueKey('geo-note-missing')), findsNothing);
      expect(find.byKey(const ValueKey('geo-matches')), findsNothing);
      await tester.pump(const Duration(seconds: 3));
      expect(tester.takeException(), isNull);
    });

    testWidgets('bases: loading with progress, loaded, a mistake and a retry ($name)', (tester) async {
      final state = await pump(tester, size);
      expect(find.byKey(const ValueKey('geo-base-geosite')), findsNothing, reason: 'a base that is loaded says nothing above the rules');

      // runetfreedom: tens of megabytes, a bar that fills.
      await useSource(tester, state, {'source': 'runetfreedom', 'geosite_url': '', 'geoip_url': '', 'presets': false});
      final text = find.byKey(const ValueKey('geo-state-geosite'));
      expect(text, findsOneWidget);
      expect(tester.widget<Text>(text).textSpan!.toPlainText(), contains('загружается… 25%'));
      expect(tester.widget<Text>(text).textSpan!.toPlainText(), contains('Сайты (geosite) · runetfreedom'));
      expect(tester.widget<LinearProgressIndicator>(find.byKey(const ValueKey('geo-progress-geosite'))).value, closeTo(.25, .001));
      // Meanwhile a name cannot be checked: the base is on its way.
      await type(tester, 'geosite:ru');
      expect(find.byKey(const ValueKey('geo-note-loading')), findsOneWidget);
      expect(find.textContaining('База ещё загружается'), findsOneWidget);

      for (var i = 0; i < 4; i++) {
        await wait(tester, 1200);
      }
      expect(find.byKey(const ValueKey('geo-progress-geosite')), findsNothing);
      expect(state.geo.base('geosite')!.state, baseLoaded);
      // The question asked meanwhile is answered once the base is there.
      await wait(tester, 400);
      expect(find.byKey(const ValueKey('geo-note-resolved')), findsOneWidget);

      // Its time, in the source of the bases.
      await tester.ensureVisible(find.text('Источник баз'));
      await tester.tap(find.text('Источник баз'));
      await tester.pump();
      await tester.ensureVisible(find.byKey(const ValueKey('geo-state-geosite')).first);
      expect(tester.widget<Text>(find.byKey(const ValueKey('geo-state-geosite')).first).textSpan!.toPlainText(), matches(RegExp(r'загружена в \d\d:\d\d')));
      expect(find.byKey(const ValueKey('geo-state-geoip')), findsOneWidget);

      // A link of one's own that does not load: the mistake, and a retry.
      await useSource(tester, state, {'source': 'custom', 'geosite_url': 'https://lists.example/fail.dat', 'geoip_url': '', 'presets': false});
      var line = tester.widget<Text>(find.byKey(const ValueKey('geo-state-geosite')).first).textSpan!.toPlainText();
      expect(line, contains('загружается…'));
      expect(line, isNot(contains('%')), reason: 'no size known: a bar without an end');
      expect(tester.widget<LinearProgressIndicator>(find.byKey(const ValueKey('geo-progress-geosite')).first).value, isNull);
      for (var i = 0; i < 4; i++) {
        await wait(tester, 1200);
      }
      line = tester.widget<Text>(find.byKey(const ValueKey('geo-state-geosite')).first).textSpan!.toPlainText();
      expect(line, contains('ошибка: through the proxy'));
      expect(line, contains('Сайты (geosite) · своя ссылка'));
      final retry = find.byKey(const ValueKey('geo-retry-geosite')).first;
      await tester.ensureVisible(retry);
      await tester.tap(retry);
      await wait(tester, 300);
      expect(tester.widget<Text>(find.byKey(const ValueKey('geo-state-geosite')).first).textSpan!.toPlainText(), contains('загружается…'));
      for (var i = 0; i < 4; i++) {
        await wait(tester, 1200);
      }
      expect(tester.widget<Text>(find.byKey(const ValueKey('geo-state-geosite')).first).textSpan!.toPlainText(), contains('загружена в'));
      expect(find.byKey(const ValueKey('geo-retry-geosite')), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 3));
    });

    testWidgets('rules mark what the base made of them ($name)', (tester) async {
      final state = await pump(tester, size);
      // Several categories fit: with runetfreedom's list both «ua» and «category-ua» do.
      await useSource(tester, state, {'source': 'runetfreedom', 'geosite_url': '', 'geoip_url': '', 'presets': false});
      await tester.runAsync(() async {});
      state.updateSettings((x) {
        x['routing']['rules'] = [
          {'match': 'geosite:ru', 'action': 'direct'},
          {'match': 'geosite:tiktoc', 'action': 'block'},
          {'match': 'geosite:u_a', 'action': 'proxy'},
          {'match': 'geosite:youtube', 'action': 'proxy'},
        ];
      });
      await wait(tester, 400);
      // The base is on its way: the rules wait for it.
      Finder row(String match) => find.byKey(ValueKey('rule-$match'));
      await tester.ensureVisible(row('geosite:ru'));
      expect(find.descendant(of: row('geosite:ru'), matching: find.text('ждёт базу')), findsOneWidget);
      expect(find.descendant(of: row('geosite:youtube'), matching: find.text('ждёт базу')), findsOneWidget);

      for (var i = 0; i < 5; i++) {
        await wait(tester, 1200);
      }
      await tester.ensureVisible(row('geosite:ru'));
      expect(find.text('ждёт базу'), findsNothing);
      // Another spelling: which category it is.
      expect(find.descendant(of: row('geosite:ru'), matching: find.text('→ category-ru')), findsOneWidget);
      // A name as it is has no mark.
      expect(
        find.descendant(of: row('geosite:youtube'), matching: find.byType(Pill)),
        findsOneWidget,
        reason: 'only the action',
      );
      // Several fit: pick one.
      await tester.ensureVisible(row('geosite:u_a'));
      expect(find.descendant(of: row('geosite:u_a'), matching: find.text('неоднозначно')), findsOneWidget);
      // A typo: no such category, and what may be meant, a tap away.
      await tester.ensureVisible(row('geosite:tiktoc'));
      expect(find.descendant(of: row('geosite:tiktoc'), matching: find.text('нет в базе')), findsOneWidget);
      await tester.tap(find.descendant(of: row('geosite:tiktoc'), matching: find.text('tiktok')));
      await wait(tester, 400);
      await wait(tester, 400);
      expect([for (final r in state.setting<List>('routing.rules', const [])) r['match']], ['geosite:ru', 'geosite:tiktok', 'geosite:u_a', 'geosite:youtube']);
      await tester.ensureVisible(row('geosite:u_a'));
      await tester.tap(find.descendant(of: row('geosite:u_a'), matching: find.text('ua')));
      await wait(tester, 400);
      await wait(tester, 400);
      expect([for (final r in state.setting<List>('routing.rules', const [])) r['match']], ['geosite:ru', 'geosite:tiktok', 'geosite:ua', 'geosite:youtube']);
      expect(tester.takeException(), isNull);
      await tester.pump(const Duration(seconds: 6));
    });
  }
}
