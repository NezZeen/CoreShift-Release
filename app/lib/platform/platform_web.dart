import '../api/backend.dart';
import '../api/demo_backend.dart';

// The web build exists only to preview the UI; browsers cannot reach the
// daemon's API, so it always runs on simulated data.
Backend createBackend() => DemoBackend();

Future<Map<String, dynamic>> loadPrefs() async => {};

Future<void> savePrefs(Map<String, dynamic> prefs) async {}

bool get canStartService => false;

Future<String?> startService() async => 'Недоступно в браузере';

Future<List<Map<String, dynamic>>> dnsLeakTest() async => throw UnsupportedError('Недоступно в браузере');

/// The system, for the journal's header.
String get osDescription => 'браузер (демо)';

bool get isAndroid => false;

Future<void> initPlatform() async {}

Future<bool> prepareVpn() async => true;
