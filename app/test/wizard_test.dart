import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/main.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/ui/theme.dart';
import 'package:coreshift/ui/widgets.dart';
import 'package:coreshift/ui/wizard/first_run_wizard.dart';
import 'package:coreshift/ui/wizard/wizard_strings.dart';

/// A subscription link as panels give them. Its token must never be shown.
const _token = 'SECRETTOKEN0123456789';
const _link = 'https://sub.example.com/api/v1/client/$_token';

/// A demo whose panels set up an automatic selection, as some do.
class _AutoDemo extends DemoBackend {
  _AutoDemo() : super(empty: true);

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    final r = await super.call(method, path, body);
    if (method == 'POST' && path == '/v1/subscriptions' && r is Map) {
      r['auto'] = [for (final n in r['nodes'] as List) n['fingerprint']];
    }
    return r;
  }
}

void main() {
  const phone = Size(390, 844);
  const desktop = Size(1400, 900);

  void clipboard(WidgetTester tester, String? text) {
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.getData') return text == null ? null : <String, dynamic>{'text': text};
      return null;
    });
    addTearDown(() => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, null));
  }

  Future<AppState> pump(WidgetTester tester, Size size, {AppState? state, bool askDisclaimer = false, Map<String, dynamic>? prefs}) async {
    tester.view.physicalSize = size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final s = state ?? AppState(DemoBackend(empty: true), askWizard: true, askDisclaimer: askDisclaimer, prefs: prefs);
    await tester.runAsync(() async {
      s.start();
      for (var i = 0; i < 50 && !s.loaded; i++) {
        await Future.delayed(const Duration(milliseconds: 20));
      }
    });
    await tester.pumpWidget(CoreShiftApp(state: s));
    await tester.pump();
    // The wizard opens a few frames after the first one.
    for (var i = 0; i < 6; i++) {
      await tester.pump(const Duration(milliseconds: 100));
    }
    return s;
  }

  /// Lets the demo service answer: its delays run on the test's clock.
  Future<void> settle(WidgetTester tester, [int ms = 3000]) async {
    for (var t = 0; t < ms; t += 100) {
      await tester.pump(const Duration(milliseconds: 100));
    }
  }

  /// A text of the wizard; the page under it has its own welcome.
  /// Puts the app away and lets the timers of the demo service and of the
  /// toasts run out.
  Future<void> finish(WidgetTester tester) async {
    await tester.pumpWidget(const SizedBox());
    for (var i = 0; i < 8; i++) {
      await tester.pump(const Duration(seconds: 1));
    }
  }

  Finder text(String t) => find.descendant(of: find.byType(FirstRunWizard), matching: find.text(t));

  /// The whole way: paste the link, check the servers, choose the mode, connect.
  Future<void> happyPath(WidgetTester tester, Size size) async {
    clipboard(tester, _link);
    final s = await pump(tester, size);
    expect(text(WizardStrings.welcomeTitle), findsOneWidget);
    expect(text(WizardStrings.welcomeStart), findsOneWidget);
    expect(text(WizardStrings.skip), findsOneWidget);

    await tester.tap(text(WizardStrings.welcomeStart));
    await settle(tester, 300);
    // The link on the clipboard is taken, and only its masked form shows.
    expect(text(WizardStrings.addTitle), findsOneWidget);
    expect(find.byKey(const ValueKey('wizard-link')), findsOneWidget);
    expect(find.textContaining(_token), findsNothing);
    expect(find.textContaining('sub.example.com'), findsOneWidget);

    await tester.tap(text(WizardStrings.addSubmit));
    await tester.pump(const Duration(milliseconds: 300));
    // Progress in words while the subscription is fetched.
    expect(text(WizardStrings.addProgress), findsOneWidget);
    await settle(tester, 1200);
    expect(s.subscriptions, hasLength(1));

    // The servers: tested, the best one chosen.
    expect(text(WizardStrings.checkTitle), findsOneWidget);
    await settle(tester, 2000);
    expect(text(WizardStrings.checkBest), findsOneWidget);
    expect(find.textContaining(' мс'), findsWidgets);
    expect(find.textContaining(_token), findsNothing);
    await tester.tap(text(WizardStrings.next));
    await settle(tester, 600);

    // The mode.
    expect(text(WizardStrings.modeTitle), findsOneWidget);
    expect(text(WizardStrings.modeAllTitle), findsOneWidget);
    expect(text(WizardStrings.modeProxyTitle), findsOneWidget);
    expect(text(WizardStrings.modeSystemProxyTitle), findsOneWidget);
    expect(text(WizardStrings.modeRecommended), findsOneWidget);
    await tester.tap(text(WizardStrings.modeProxyTitle));
    await tester.pump();
    await tester.tap(text(WizardStrings.next));
    await settle(tester, 600);
    expect(s.setting('tun', true), isFalse);
    expect(s.setting('proxy.system', true), isFalse);

    // Done: connect.
    expect(text(WizardStrings.doneTitle), findsOneWidget);
    expect(text(WizardStrings.doneLater), findsOneWidget);
    await tester.tap(text(WizardStrings.doneConnect));
    await settle(tester, 2500);
    expect(find.byType(FirstRunWizard), findsNothing);
    expect(s.prefs[wizardPref], isTrue);
    expect(s.status.active, isTrue);
    expect(s.selection.name, isNotEmpty);

    await tester.runAsync(() => s.disconnect());
    await finish(tester);
  }

  group('when it is shown', () {
    testWidgets('a fresh install sees it right after the disclaimer, and not before', (tester) async {
      clipboard(tester, null);
      final s = await pump(tester, desktop, askDisclaimer: true);
      expect(find.text('Отказ от ответственности'), findsOneWidget);
      expect(find.byType(FirstRunWizard), findsNothing);
      await tester.tap(find.text('Принимаю'));
      await settle(tester, 800);
      expect(find.byType(FirstRunWizard), findsOneWidget);
      expect(text(WizardStrings.welcomeTitle), findsOneWidget);
      expect(s.prefs[wizardPref], isNull, reason: 'remembered when it ends, not when it opens');
    });

    testWidgets('a user with subscriptions never sees it', (tester) async {
      clipboard(tester, null);
      final s = await pump(tester, desktop, state: AppState(DemoBackend(), askWizard: true));
      expect(s.subscriptions, isNotEmpty);
      expect(find.byType(FirstRunWizard), findsNothing);
      // Not even after removing every subscription.
      expect(s.prefs[wizardPref], isTrue);
    });

    testWidgets('a state that does not ask for it never shows it', (tester) async {
      clipboard(tester, null);
      await pump(tester, desktop, state: AppState(DemoBackend(empty: true)));
      expect(find.byType(FirstRunWizard), findsNothing);
    });

    for (final size in [phone, desktop]) {
      testWidgets('skip works and is remembered (${size.width.toInt()}x${size.height.toInt()})', (tester) async {
        clipboard(tester, null);
        final saved = <Map<String, dynamic>>[];
        final prefs = <String, dynamic>{};
        final s = await pump(
          tester,
          size,
          state: AppState(DemoBackend(empty: true), askWizard: true, prefs: prefs, savePrefs: (p) async => saved.add({...p})),
        );
        expect(find.byType(FirstRunWizard), findsOneWidget);
        await tester.tap(text(WizardStrings.skip));
        await settle(tester, 600);
        expect(find.byType(FirstRunWizard), findsNothing);
        expect(prefs[wizardPref], isTrue);
        expect(saved.last[wizardPref], isTrue);
        expect(s.subscriptions, isEmpty);

        // The next start of the app, with the same prefs.
        await tester.pumpWidget(const SizedBox());
        final again = await pump(tester, size, state: AppState(DemoBackend(empty: true), askWizard: true, prefs: prefs));
        expect(again.loaded, isTrue);
        expect(find.byType(FirstRunWizard), findsNothing);
      });
    }

    testWidgets('Esc skips and Enter goes on', (tester) async {
      clipboard(tester, null);
      final s = await pump(tester, desktop);
      await tester.sendKeyEvent(LogicalKeyboardKey.enter);
      await settle(tester, 300);
      expect(text(WizardStrings.addTitle), findsOneWidget);
      await tester.sendKeyEvent(LogicalKeyboardKey.escape);
      await settle(tester, 600);
      expect(find.byType(FirstRunWizard), findsNothing);
      expect(s.prefs[wizardPref], isTrue);
    });

    testWidgets('Android back goes a step back, and from the first one skips', (tester) async {
      clipboard(tester, null);
      final s = await pump(tester, phone);
      await tester.tap(text(WizardStrings.welcomeStart));
      await settle(tester, 300);
      expect(text(WizardStrings.addTitle), findsOneWidget);
      await tester.binding.handlePopRoute();
      await settle(tester, 300);
      expect(text(WizardStrings.welcomeTitle), findsOneWidget);
      await tester.binding.handlePopRoute();
      await settle(tester, 600);
      expect(find.byType(FirstRunWizard), findsNothing);
      expect(s.prefs[wizardPref], isTrue);
    });
  });

  group('the way through', () {
    testWidgets('phone: paste, servers, mode, connect', (tester) async {
      await happyPath(tester, phone);
    });

    testWidgets('desktop: paste, servers, mode, connect', (tester) async {
      await happyPath(tester, desktop);
    });

    testWidgets('«Вставить» takes a link from the clipboard and does not show it', (tester) async {
      clipboard(tester, null);
      await pump(tester, phone);
      await tester.tap(text(WizardStrings.welcomeStart));
      await settle(tester, 300);
      expect(find.byKey(const ValueKey('wizard-link')), findsNothing);
      await tester.tap(text(WizardStrings.addPaste));
      await settle(tester, 100);
      expect(text(WizardStrings.addClipboardEmpty), findsOneWidget);

      clipboard(tester, '  $_link\n');
      await tester.tap(text(WizardStrings.addPaste));
      await settle(tester, 100);
      expect(find.byKey(const ValueKey('wizard-link')), findsOneWidget);
      expect(find.textContaining(_token), findsNothing);
      expect(find.textContaining('sub.example.com'), findsOneWidget);
      // «Изменить» brings the empty field back.
      await tester.tap(text(WizardStrings.addClear));
      await tester.pump();
      expect(find.byKey(const ValueKey('wizard-field')), findsOneWidget);
    });

    testWidgets('a link pasted into the field is taken out of it', (tester) async {
      clipboard(tester, null);
      await pump(tester, desktop);
      await tester.tap(text(WizardStrings.welcomeStart));
      await settle(tester, 300);
      await tester.enterText(find.byKey(const ValueKey('wizard-field')), _link);
      await tester.pump();
      expect(find.textContaining(_token), findsNothing);
      expect(find.byKey(const ValueKey('wizard-link')), findsOneWidget);
    });

    testWidgets('a refused subscription says why, and the link stays hidden', (tester) async {
      clipboard(tester, 'https://sub.example.com/api/v1/fail/$_token');
      final s = await pump(tester, phone);
      await tester.tap(text(WizardStrings.welcomeStart));
      await settle(tester, 300);
      await tester.tap(text(WizardStrings.addSubmit));
      await settle(tester, 1200);
      expect(s.subscriptions, isEmpty);
      expect(text(WizardStrings.addTitle), findsOneWidget);
      expect(find.byIcon(Icons.error_outline), findsOneWidget);
      expect(find.textContaining(_token), findsNothing);
      // It can be tried again.
      expect(tester.widget<Btn>(find.widgetWithText(Btn, WizardStrings.addSubmit)).onPressed, isNotNull);
    });

    testWidgets('«Добавить сервер вручную» ends the wizard with the usual add dialog', (tester) async {
      clipboard(tester, null);
      final s = await pump(tester, desktop);
      await tester.tap(text(WizardStrings.welcomeStart));
      await settle(tester, 300);
      await tester.tap(text(WizardStrings.addManual));
      await settle(tester, 600);
      expect(find.byType(FirstRunWizard), findsNothing);
      expect(find.text('Добавить подписку'), findsWidgets);
      expect(s.prefs[wizardPref], isTrue);
    });

    testWidgets('a subscription with automatic selection is chosen as «Автовыбор»', (tester) async {
      clipboard(tester, _link);
      final s = await pump(tester, desktop, state: AppState(_AutoDemo(), askWizard: true));
      await tester.tap(text(WizardStrings.welcomeStart));
      await settle(tester, 300);
      await tester.tap(text(WizardStrings.addSubmit));
      await settle(tester, 3500);
      expect(find.byKey(const ValueKey('wizard-choice-auto')), findsOneWidget);
      expect(find.descendant(of: find.byKey(const ValueKey('wizard-choice-auto')), matching: find.byIcon(Icons.radio_button_checked)), findsOneWidget);
      await tester.tap(text(WizardStrings.next));
      await settle(tester, 600);
      await tester.tap(text(WizardStrings.next));
      await settle(tester, 600);
      expect(text(WizardStrings.checkAuto), findsOneWidget, reason: 'the summary says so');
      expect(s.selection.name, isNotEmpty);
      await tester.tap(text(WizardStrings.doneLater));
      await settle(tester, 600);
      await finish(tester);
    });

    testWidgets('Android has the VPN and the proxy without it', (tester) async {
      clipboard(tester, _link);
      tester.view.physicalSize = phone;
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);
      final s = AppState(DemoBackend(empty: true), askWizard: true);
      await tester.runAsync(() async {
        s.start();
        for (var i = 0; i < 50 && !s.loaded; i++) {
          await Future.delayed(const Duration(milliseconds: 20));
        }
      });
      await tester.pumpWidget(
        MaterialApp(
          theme: buildTheme(Brightness.dark),
          home: Builder(
            builder: (context) => Scaffold(
              body: TextButton(onPressed: () => showFirstRunWizard(context, s, android: true), child: const Text('open')),
            ),
          ),
        ),
      );
      await tester.tap(find.text('open'));
      await settle(tester, 400);
      await tester.tap(text(WizardStrings.welcomeStart));
      await settle(tester, 300);
      await tester.tap(text(WizardStrings.addSubmit));
      await settle(tester, 3500);
      await tester.tap(text(WizardStrings.next));
      await settle(tester, 600);
      expect(text(WizardStrings.modeVpnTitle), findsOneWidget);
      expect(text(WizardStrings.modeAllTitle), findsNothing);
      expect(text(WizardStrings.modeProxyTitle), findsNothing);
      expect(text(WizardStrings.modeSystemProxyTitle), findsNothing);
      expect(text(WizardStrings.modeProxyOnlyTitle), findsOneWidget);
      expect(text(WizardStrings.modeRecommended), findsOneWidget);
      await tester.tap(text(WizardStrings.modeProxyOnlyTitle));
      await tester.pump();
      await tester.tap(text(WizardStrings.next));
      await settle(tester, 600);
      expect(s.setting('tun', true), isFalse, reason: 'the proxy without the VPN is TUN off');
      await tester.tap(text(WizardStrings.doneLater));
      await settle(tester, 600);
      await finish(tester);
    });
  });

  group('with the other windows', () {
    testWidgets('the clipboard offer does not come over the wizard, and comes after it if skipped', (tester) async {
      clipboard(tester, _link);
      final s = await pump(tester, desktop);
      expect(find.byType(FirstRunWizard), findsOneWidget);
      // The clipboard (or a panel's button) hands the shell a link.
      await tester.runAsync(() => s.offerImport(_link, ImportFrom.clipboard));
      await settle(tester, 600);
      expect(s.pendingImport, isNotNull);
      expect(find.text('Добавить подписку?'), findsNothing);
      expect(find.byType(FirstRunWizard), findsOneWidget);

      // Skipped: what waited is offered.
      await tester.tap(text(WizardStrings.skip));
      await settle(tester, 800);
      expect(find.byType(FirstRunWizard), findsNothing);
      expect(find.text('Добавить подписку?'), findsOneWidget);
    });

    testWidgets('the link the wizard took from the clipboard is not offered again', (tester) async {
      clipboard(tester, _link);
      final s = await pump(tester, desktop);
      await tester.tap(text(WizardStrings.welcomeStart));
      await settle(tester, 300);
      expect(find.byKey(const ValueKey('wizard-link')), findsOneWidget);
      await tester.tap(text(WizardStrings.skip));
      await settle(tester, 600);
      // The same link offered from the clipboard later is already known.
      await tester.runAsync(() => s.offerImport(_link, ImportFrom.clipboard));
      await settle(tester, 300);
      expect(s.pendingImport, isNull);
      expect(find.text('Добавить подписку?'), findsNothing);
    });
  });
}
