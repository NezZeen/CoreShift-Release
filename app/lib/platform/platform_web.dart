import 'dart:typed_data';

import '../api/backend.dart';
import '../api/demo_backend.dart';
import 'vpn_consent.dart';

// The web build exists only to preview the UI; browsers cannot reach the
// daemon's API, so it always runs on simulated data.
Backend createBackend() => DemoBackend();

Future<Map<String, dynamic>> loadPrefs() async => {};

Future<void> savePrefs(Map<String, dynamic> prefs) async {}

bool get canStartService => false;

bool daemonAccessDenied(String reason) => false;

bool Function()? get daemonStarter => null;

bool Function(bool on)? get autostartSetter => null;

Future<String?> startService() async => 'Недоступно в браузере';

/// The system, for the journal's header.
String get osDescription => 'браузер (демо)';

bool get isAndroid => false;

bool get isLinux => false;

bool get isWindows => false;

Future<void> initPlatform() async {}

Future<VpnConsent> prepareVpn() async => VpnConsent.granted;

Future<bool> openVpnSettings() async => false;

Future<bool> canInstallUpdates() async => true;

Future<void> allowInstallUpdates() async {}

Future<bool> openUrl(String url) async => false;

Future<void> quitApp() async {}

Future<String?> initialLink(List<String> args) async => null;

void onLink(void Function(String link) handler) {}

bool get canScanQr => false;

Future<String?> scanQr() async => null;

String scanQrError(Object e) => 'Сканер QR-кодов недоступен';

Future<void> notify(String title, String body) async {}

typedef AndroidApp = ({String package, String label, bool system});

Future<List<AndroidApp>> installedApps() async => [];

Future<Uint8List?> appIcon(String package) async => null;
