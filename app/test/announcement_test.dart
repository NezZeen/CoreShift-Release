import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/announcements.dart';
import 'package:coreshift/state/app_state.dart';

void main() {
  const sizes = {'phone': Size(390, 844), 'desktop': Size(1400, 900)};
  const demoText = 'В пятницу с 02:00 до 04:00 по Москве обновляем серверы в Германии.';

  Future<AppState> pumpApp(WidgetTester tester, Size size, {List<String>? opened, Map<String, dynamic>? prefs}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final state = AppState(
      DemoBackend(),
      prefs: prefs,
      linkOpener: (url) async {
        opened?.add(url);
        return true;
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
    return state;
  }

  /// Lets the app's timers run out, as the other tests do at their end.
  Future<void> settle(WidgetTester tester) => tester.pump(const Duration(seconds: 30));

  /// Tells the pages the subscriptions changed, as a refresh does.
  void redraw(AppState state) => state.setPref('redraw', DateTime.now().microsecondsSinceEpoch);

  /// The first subscription with another announcement (null clears it).
  void announce(AppState state, String? text, {String url = ''}) {
    final base = state.subscriptions.first;
    final i = base.info;
    state.subscriptions = [
      Subscription(
        id: base.id,
        name: base.name,
        displayName: base.displayName,
        url: base.url,
        userAgent: '',
        info: SubInfo(title: i.title, supportUrl: i.supportUrl, webPageUrl: i.webPageUrl, announce: text ?? '', announceUrl: url),
        format: base.format,
        nodes: base.nodes,
        skipped: const [],
      ),
      ...state.subscriptions.skip(1),
    ];
  }

  for (final MapEntry(key: where, value: size) in sizes.entries) {
    testWidgets('the provider\'s announcement on the $where: shown, hidden, and a new text shows again', (tester) async {
      final opened = <String>[];
      final state = await pumpApp(tester, size, opened: opened);
      expect(state.subscriptions.first.info.announce, startsWith(demoText));

      // The card: the subscription's name, the text and the way to read on.
      expect(find.text('Объявление от «NorthLink Premium»'), findsOneWidget);
      expect(find.textContaining(demoText), findsOneWidget);
      expect(find.text('Подробнее'), findsOneWidget);
      expect(find.text('Скрыть'), findsOneWidget);
      expect(tester.takeException(), isNull, reason: where);

      await tester.tap(find.text('Подробнее'));
      await tester.pump();
      expect(opened, ['https://northlink.example/news/maintenance']);

      // Hidden: gone, and remembered with the preferences.
      await tester.tap(find.text('Скрыть'));
      await tester.pump();
      expect(find.textContaining(demoText), findsNothing);
      expect(find.text('Скрыть'), findsNothing);
      expect(state.announcements, isEmpty);
      expect((state.prefs['announce_hidden'] as Map).keys, [state.subscriptions.first.id]);

      // The panel sends the same text again with the next refresh: still hidden.
      announce(state, state.subscriptions.first.info.announce);
      redraw(state);
      await tester.pump();
      expect(find.text('Объявление от «NorthLink Premium»'), findsNothing);

      // A new text shows again.
      announce(state, 'Новый сервер в Финляндии. Подключайтесь!');
      redraw(state);
      await tester.pump();
      expect(find.text('Объявление от «NorthLink Premium»'), findsOneWidget);
      expect(find.text('Новый сервер в Финляндии. Подключайтесь!'), findsOneWidget);
      // Without a link, no «Подробнее».
      expect(find.text('Подробнее'), findsNothing);

      // The panel withdraws it.
      announce(state, null);
      redraw(state);
      await tester.pump();
      expect(find.text('Скрыть'), findsNothing);
      expect(tester.takeException(), isNull, reason: where);
      await settle(tester);
    });

    testWidgets('a hidden announcement stays hidden after a restart on the $where', (tester) async {
      final first = await pumpApp(tester, size);
      final hidden = <String, dynamic>{};
      await tester.tap(find.text('Скрыть'));
      await tester.pump();
      hidden.addAll(first.prefs);
      expect(hidden['announce_hidden'], isNotEmpty);

      // A new run of the app with the preferences saved.
      final second = await pumpApp(tester, size, prefs: hidden);
      expect(second.announcements, isEmpty);
      expect(find.textContaining(demoText), findsNothing);
      await settle(tester);
    });

    testWidgets('a long announcement is cut and shown whole on request on the $where', (tester) async {
      final state = await pumpApp(tester, size);
      final long = List.generate(30, (i) => 'Строка номер $i объявления.').join(' ');
      announce(state, long);
      redraw(state);
      await tester.pump();
      expect(find.text('Показать полностью'), findsOneWidget);
      await tester.tap(find.text('Показать полностью'));
      await tester.pump();
      expect(find.text('Свернуть'), findsOneWidget);
      expect(tester.takeException(), isNull, reason: where);
      await settle(tester);
    });
  }

  testWidgets('the subscription shows «Сайт» beside «Поддержка» when the panel sent a page', (tester) async {
    for (final size in sizes.values) {
      final opened = <String>[];
      await pumpApp(tester, size, opened: opened);
      await tester.tap(find.text('Серверы').last);
      await tester.pump();
      expect(find.text('Поддержка'), findsWidgets);
      final site = find.byIcon(Icons.language);
      expect(site, findsOneWidget);
      await tester.tap(site);
      await tester.pump();
      expect(opened, ['https://northlink.example/account']);
      expect(tester.takeException(), isNull, reason: '$size');
      await settle(tester);
    }
  });
}
