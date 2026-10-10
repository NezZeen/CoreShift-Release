import 'package:flutter/material.dart';

import 'api/backend.dart';
import 'api/demo_backend.dart';
import 'platform/desktop.dart' as desktop;
import 'platform/platform.dart' as platform;
import 'state/app_state.dart';
import 'ui/shell.dart';
import 'ui/theme.dart';

/// `flutter run --dart-define=DEMO=true` runs on simulated data, without the
/// daemon.
const demo = bool.fromEnvironment('DEMO');

/// `--dart-define=DEMO_EMPTY=true` with DEMO: the demo starts as a fresh
/// install, with the first-run wizard.
const demoEmpty = bool.fromEnvironment('DEMO_EMPTY');

/// `--tray` starts hidden in the tray: the service starts the app so after
/// updating CoreShift, and Windows at sign-in ("Автозапуск").
Future<void> main(List<String> args) async {
  WidgetsFlutterBinding.ensureInitialized();
  await desktop.initWindow(hidden: args.contains('--tray'));
  if (!demo) await platform.initPlatform();
  final prefs = await platform.loadPrefs();
  final Backend backend = demo ? DemoBackend(empty: demoEmpty) : platform.createBackend();
  final state = AppState(
    backend,
    prefs: prefs,
    savePrefs: platform.savePrefs,
    daemonStarter: demo ? null : platform.daemonStarter,
    autostartSetter: demo ? null : platform.autostartSetter,
    askDisclaimer: true,
    askWizard: true,
    journalSaver: demo ? null : platform.saveJournal,
  )..start();
  // Links from a panel's "add to app" button: the one CoreShift was opened
  // with, and those opened while it runs.
  platform.onLink((link) => state.offerImport(link, ImportFrom.link));
  final link = await platform.initialLink(args);
  if (link != null) state.offerImport(link, ImportFrom.link);
  runApp(CoreShiftApp(state: state));
}

class CoreShiftApp extends StatefulWidget {
  final AppState state;
  const CoreShiftApp({super.key, required this.state});

  @override
  State<CoreShiftApp> createState() => _CoreShiftAppState();
}

class _CoreShiftAppState extends State<CoreShiftApp> {
  late ThemeMode themeMode = ThemeMode.values.asNameMap()[widget.state.prefs['theme']] ?? ThemeMode.dark;

  void _setTheme(ThemeMode m) {
    setState(() => themeMode = m);
    widget.state.setPref('theme', m.name);
  }

  @override
  void dispose() {
    widget.state.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'CoreShift',
      debugShowCheckedModeBanner: false,
      theme: buildTheme(Brightness.light),
      darkTheme: buildTheme(Brightness.dark),
      themeMode: themeMode,
      // The colours blend from one theme into the other.
      themeAnimationDuration: const Duration(milliseconds: 450),
      themeAnimationCurve: Curves.easeInOut,
      builder: (context, child) => desktop.DesktopFrame(state: widget.state, child: child!),
      home: Shell(state: widget.state, themeMode: themeMode, onThemeMode: _setTheme),
    );
  }
}
