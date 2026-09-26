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

/// `--tray` starts hidden in the tray: the service starts the app so after
/// updating CoreShift.
Future<void> main(List<String> args) async {
  WidgetsFlutterBinding.ensureInitialized();
  await desktop.initWindow(hidden: args.contains('--tray'));
  if (!demo) await platform.initPlatform();
  final prefs = await platform.loadPrefs();
  final Backend backend = demo ? DemoBackend() : platform.createBackend();
  runApp(
    CoreShiftApp(
      state: AppState(backend, prefs: prefs, savePrefs: platform.savePrefs)..start(),
    ),
  );
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
      builder: (context, child) => desktop.DesktopFrame(state: widget.state, child: child!),
      home: Shell(state: widget.state, themeMode: themeMode, onThemeMode: _setTheme),
    );
  }
}
