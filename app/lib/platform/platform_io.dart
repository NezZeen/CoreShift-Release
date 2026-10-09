import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

import '../api/backend.dart';
import '../api/models.dart';
import '../api/proof.dart';
import 'api_access.dart';
import 'linux_desktop.dart' as linux;
import 'vpn_consent.dart';
import 'win_service.dart' as win;

/// On Android the engine runs inside the app; this channel reaches its
/// Kotlin side (MainActivity.kt).
const _android = MethodChannel('coreshift/android');

/// Where the Android engine keeps its API file; set by [initPlatform].
String? _androidApiFile;

bool get isAndroid => Platform.isAndroid;

bool get isLinux => Platform.isLinux;

bool get isWindows => Platform.isWindows;

/// What runs the VPN, for messages: a Windows service, or on Android the
/// engine inside the app.
String get _engine => Platform.isAndroid ? 'Движок CoreShift' : 'Служба CoreShift';
String get _ending => Platform.isAndroid ? '' : 'а';

/// Asks the platform what the app needs before it starts.
Future<void> initPlatform() async {
  if (Platform.isAndroid) _androidApiFile = await _android.invokeMethod<String>('apiFile');
}

/// Gets the user's consent to the VPN when Android needs it, and says how
/// Android answered. Elsewhere the service has the rights it needs.
Future<VpnConsent> prepareVpn() async {
  if (!Platform.isAndroid) return VpnConsent.granted;
  return switch (await _android.invokeMethod<Object>('prepareVpn')) {
    true || 'granted' => VpnConsent.granted,
    'unasked' => VpnConsent.unasked,
    _ => VpnConsent.denied,
  };
}

/// Opens Android's VPN settings, where another app's "always-on" VPN is
/// turned off; false when no settings screen could be opened.
Future<bool> openVpnSettings() async {
  if (!Platform.isAndroid) return false;
  try {
    return await _android.invokeMethod<bool>('openVpnSettings') ?? false;
  } catch (_) {
    return false;
  }
}

/// Whether Android lets the app install its updates. Elsewhere the service
/// installs them itself.
Future<bool> canInstallUpdates() async {
  if (!Platform.isAndroid) return true;
  return await _android.invokeMethod<bool>('canInstallUpdates') ?? false;
}

/// Opens Android's screen where the user lets CoreShift install updates.
Future<void> allowInstallUpdates() async {
  if (Platform.isAndroid) await _android.invokeMethod<void>('allowInstallUpdates');
}

/// An app installed on the phone, for the per-app VPN.
typedef AndroidApp = ({String package, String label, bool system});

/// The phone's apps with a launcher icon, but CoreShift.
Future<List<AndroidApp>> installedApps() async {
  if (!Platform.isAndroid) return [];
  final list = await _android.invokeListMethod<Map>('apps') ?? const [];
  return [for (final m in list) (package: m['package'] as String, label: m['label'] as String, system: m['system'] == true)];
}

/// An app's icon as a PNG; null when it has none.
Future<Uint8List?> appIcon(String package) async {
  if (!Platform.isAndroid) return null;
  try {
    return await _android.invokeMethod<Uint8List>('appIcon', package);
  } catch (_) {
    return null;
  }
}

/// On Windows the running app hears of links opened later through this
/// channel (windows/runner: a second start hands its link over).
const _desktop = MethodChannel('coreshift/desktop');

/// The link CoreShift was opened with (coreshift://…, a panel's button),
/// from the command line or Android's intent; null when there is none.
Future<String?> initialLink(List<String> args) async {
  if (Platform.isAndroid) {
    try {
      return await _android.invokeMethod<String>('initialLink');
    } catch (_) {
      return null;
    }
  }
  return args.where((a) => a.contains('://')).firstOrNull;
}

/// Calls [handler] with each link opened while the app runs.
void onLink(void Function(String link) handler) {
  final channel = Platform.isAndroid ? _android : (Platform.isWindows || Platform.isLinux ? _desktop : null);
  channel?.setMethodCallHandler((call) async {
    if (call.method == 'openLink' && call.arguments is String) handler(call.arguments as String);
  });
}

/// Whether the phone can scan a QR code for the app.
bool get canScanQr => Platform.isAndroid;

/// Scans a QR code with Google's scanner (Play services); returns its text,
/// or null when the user went back. Throws when the scanner is missing.
Future<String?> scanQr() => _android.invokeMethod<String>('scanQr');

/// What to tell the user when [scanQr] failed with [e].
String scanQrError(Object e) => e is PlatformException && e.code == 'unavailable'
    ? 'Сканер QR-кодов ещё загружается сервисами Google Play. Проверьте интернет и попробуйте через минуту'
    : 'Сканер QR-кодов недоступен: на телефоне нет сервисов Google Play';

/// Shows an Android notification; on the desktop the tray does (see
/// desktop_io.dart), and only while the window is out of sight.
Future<void> notify(String title, String body) async {
  if (!Platform.isAndroid) return;
  try {
    await _android.invokeMethod<void>('notify', {'title': title, 'body': body});
  } catch (_) {
    // A notification is a courtesy; the app shows the same itself.
  }
}

/// Opens a link in the browser or the app that handles it (Telegram for
/// t.me); false when nothing could. The caller checks the link.
Future<bool> openUrl(String url) async {
  if (Platform.isAndroid) return await _android.invokeMethod<bool>('openUrl', url) ?? false;
  final r = Platform.isWindows ? await Process.run('rundll32.exe', ['url.dll,FileProtocolHandler', url]) : await Process.run('xdg-open', [url]);
  return r.exitCode == 0;
}

Backend createBackend() => HttpBackend(apiFile());

/// The file where the daemon publishes its address and token.
String apiFile() {
  final override = Platform.environment['CORESHIFT_API_FILE'];
  if (override != null && override.isNotEmpty) return override;
  if (Platform.isAndroid) return _androidApiFile ?? '';
  if (Platform.isWindows) {
    final pd = Platform.environment['ProgramData'] ?? r'C:\ProgramData';
    return '$pd\\CoreShift\\api.json';
  }
  return '/var/lib/coreshift/api.json';
}

class _Endpoint {
  final Uri base;
  final String token;
  const _Endpoint(this.base, this.token);
}

/// Whether [reason], why the daemon is offline, is that the user may not
/// reach it (not in the group of the API file: coreshift on Linux,
/// «CoreShift Users» on Windows), which starting it again would not change.
bool daemonAccessDenied(String reason) => (Platform.isLinux || Platform.isWindows) && reason.startsWith(accessDeniedPrefix);

/// Why the API file [f] could not be read, for the user.
DaemonOffline _unreadable(File f, FileSystemException e) {
  final code = e.osError?.errorCode;
  if (Platform.isLinux) {
    // ENOENT: the daemon is not running (it removes the file on exit).
    if (code == 2) return DaemonOffline('$_engine не запущен$_ending');
    // EACCES, EPERM: not in the group the file is for.
    if (code == 13 || code == 1) return const DaemonOffline(linux.groupHint, accessDenied: true);
  }
  if (Platform.isWindows) {
    // ERROR_FILE_NOT_FOUND: removed between the check and the read.
    if (code == 2) return DaemonOffline('$_engine не запущен$_ending');
    // ERROR_ACCESS_DENIED: not in the group the file is for.
    if (code == 5) return DaemonOffline(windowsGroupHint(windowsAccount(Platform.environment)), accessDenied: true);
  }
  return DaemonOffline('Нет доступа к ${f.path}: ${e.osError?.message ?? e.message}');
}

/// What to say when whoever answers at the address of api.json does not
/// prove it knows the token: not the service that wrote the file.
String get _impostor =>
    'По адресу из ${Platform.isAndroid ? 'файла движка' : 'файла службы'} отвечает другая программа, а не $_engineLower. '
    '${Platform.isAndroid ? 'Перезапустите CoreShift' : 'Перезапустите CoreShift или компьютер'}';

String get _engineLower => Platform.isAndroid ? 'движок CoreShift' : 'служба CoreShift';

class HttpBackend implements Backend {
  final String file;
  // DIRECT: by default Dart takes a proxy from HTTP_PROXY, which some
  // users set for other tools, and does not exempt 127.0.0.1 from it.
  final HttpClient _client = HttpClient()
    ..connectionTimeout = const Duration(seconds: 3)
    ..findProxy = ((_) => 'DIRECT');
  _Endpoint? _ep;

  HttpBackend(this.file);

  @override
  String get description => _ep?.base.authority ?? file;

  /// The address and token of api.json, once whoever answers there proved
  /// it knows the token: a file left by a service that crashed may name a
  /// port another program holds now, which must get nothing from the app.
  Future<_Endpoint> _endpoint({bool reload = false}) async {
    if (_ep != null && !reload) return _ep!;
    _ep = null;
    final ep = await _readFile();
    await _verify(ep);
    return _ep = ep;
  }

  Future<_Endpoint> _readFile() async {
    final f = File(file);
    // On Linux the file is in a directory only the coreshift group may
    // enter: there a missing file and a refused one look alike to exists().
    if (!Platform.isLinux && !await f.exists()) {
      throw DaemonOffline('$_engine не запущен$_ending');
    }
    try {
      final j = jsonDecode(await f.readAsString()) as Map<String, dynamic>;
      return _Endpoint(Uri.parse(j['address'] as String), j['token'] as String);
    } on FileSystemException catch (e) {
      throw _unreadable(f, e);
    } catch (_) {
      throw DaemonOffline('Файл ${f.path} повреждён');
    }
  }

  /// Asks the service at [ep] to prove it knows the token, before anything
  /// else is sent there.
  Future<void> _verify(_Endpoint ep) async {
    try {
      final nonce = newNonce();
      final req = await _client.getUrl(ep.base.resolve('/v1/hello'));
      req.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${ep.token}');
      req.headers.set(nonceHeader, nonce);
      final resp = await req.close();
      await resp.drain<void>();
      // A service that started anew has a new token and has replaced the
      // file by now: the next attempt reads it.
      if (resp.statusCode == HttpStatus.unauthorized) throw DaemonOffline('$_engine не отвечает');
      if (!proofValid(resp.headers.value(proofHeader), ep.token, nonce)) throw DaemonOffline(_impostor);
    } on SocketException {
      throw DaemonOffline('$_engine не отвечает');
    } on HttpException {
      throw DaemonOffline('$_engine не отвечает');
    }
  }

  /// Checks the proof of a response to a request that carried [nonce]; a
  /// response without it did not come from the service.
  void _check(HttpClientResponse resp, _Endpoint ep, String nonce) {
    if (proofValid(resp.headers.value(proofHeader), ep.token, nonce)) return;
    _ep = null;
    throw DaemonOffline(_impostor);
  }

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    // A second attempt re-reads the API file: the daemon may have restarted
    // with a new port or token.
    for (var attempt = 0; ; attempt++) {
      final ep = await _endpoint(reload: attempt > 0);
      try {
        final nonce = newNonce();
        final req = await _client.openUrl(method, ep.base.resolve(path));
        req.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${ep.token}');
        req.headers.set(nonceHeader, nonce);
        if (body != null) {
          req.headers.contentType = ContentType.json;
          req.add(utf8.encode(jsonEncode(body)));
        }
        final resp = await req.close();
        final text = await resp.transform(utf8.decoder).join();
        if (resp.statusCode == HttpStatus.unauthorized && attempt == 0) continue;
        if (!proofValid(resp.headers.value(proofHeader), ep.token, nonce) && resp.statusCode != HttpStatus.unauthorized) {
          // Not the service: it may have restarted elsewhere, which the
          // file says; else whoever holds the port gets nothing more.
          _ep = null;
          if (attempt == 0) continue;
          throw DaemonOffline(_impostor);
        }
        final decoded = text.trim().isEmpty ? null : jsonDecode(text);
        if (resp.statusCode >= 400) {
          final msg = decoded is Map && decoded['error'] is String ? decoded['error'] as String : 'HTTP ${resp.statusCode}';
          throw ApiError(resp.statusCode, msg);
        }
        return decoded;
      } on SocketException {
        if (attempt == 0) continue;
        throw DaemonOffline('$_engine не отвечает');
      } on HttpException {
        if (attempt == 0) continue;
        throw DaemonOffline('Соединение ${Platform.isAndroid ? 'с движком' : 'со службой'} прервалось');
      }
    }
  }

  @override
  Stream<Event> events() {
    // A plain controller rather than async*: cancelling must abort the
    // request at once, while an async* generator would wait for the
    // endless response to end.
    HttpClientRequest? req;
    StreamSubscription<String>? lines;
    var cancelled = false;
    late final StreamController<Event> out;
    out = StreamController<Event>(
      onListen: () async {
        try {
          final ep = await _endpoint(reload: true);
          final nonce = newNonce();
          req = await _client.getUrl(ep.base.resolve('/v1/events?replay=1&app=1'));
          req!.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${ep.token}');
          req!.headers.set(nonceHeader, nonce);
          final resp = await req!.close();
          if (cancelled) return;
          // The service refuses a stale token before it proves anything.
          if (resp.statusCode != HttpStatus.unauthorized) _check(resp, ep, nonce);
          if (resp.statusCode != HttpStatus.ok) {
            throw ApiError(resp.statusCode, 'event stream: HTTP ${resp.statusCode}');
          }
          lines = resp
              .transform(utf8.decoder)
              .transform(const LineSplitter())
              .listen(
                (line) {
                  if (!line.startsWith('data: ')) return;
                  try {
                    out.add(Event.fromJson(jsonDecode(line.substring(6)) as Map<String, dynamic>));
                  } catch (_) {
                    // One unreadable event must not end the stream.
                  }
                },
                onError: (Object e) => out.addError(DaemonOffline('Соединение ${Platform.isAndroid ? 'с движком' : 'со службой'} прервалось')),
                onDone: out.close,
              );
        } catch (e) {
          if (cancelled) return;
          out.addError(e is SocketException || e is HttpException ? DaemonOffline('$_engine не отвечает') : e);
          out.close();
        }
      },
      onCancel: () {
        cancelled = true;
        req?.abort();
        lines?.cancel();
      },
    );
    return out.stream;
  }
}

/// Starts the Windows service, which runs only while the app does, without
/// administrator rights; null where there is no such service. The Linux
/// service runs from boot (systemd), so there is nothing to start quietly.
bool Function()? get daemonStarter => Platform.isWindows ? win.startServiceQuietly : null;

/// Adds the app to the programs the system starts at sign-in, or removes
/// it; null where CoreShift does not (Android starts it at boot itself).
/// On Linux it is an XDG autostart entry.
bool Function(bool on)? get autostartSetter => Platform.isWindows
    ? win.setAutostart
    : Platform.isLinux
    ? (on) => linux.setAutostart(on)
    : null;

/// Whether the UI can start a stopped service itself (with a UAC prompt on
/// Windows, polkit's password dialog on Linux).
bool get canStartService => Platform.isWindows || Platform.isLinux;

/// Starts the CoreShift service, asking for administrator rights.
/// Returns why it could not, or null once the request went through.
Future<String?> startService() async {
  if (Platform.isLinux) return linux.startService();
  if (!Platform.isWindows) return 'Запустите службу coreshift вручную';
  try {
    final r = await Process.run('powershell.exe', [
      '-NoProfile',
      '-NonInteractive',
      '-Command',
      "Start-Process -FilePath sc.exe -ArgumentList 'start','CoreShift' -Verb RunAs -WindowStyle Hidden",
    ]);
    // Declining the UAC prompt makes Start-Process fail.
    return r.exitCode == 0 ? null : 'Запуск отменён или не удался';
  } catch (e) {
    return '$e';
  }
}

File _prefsFile() {
  if (Platform.isAndroid) return File('${File(apiFile()).parent.parent.path}/ui.json');
  if (Platform.isWindows) {
    final appData = Platform.environment['APPDATA'] ?? '.';
    return File('$appData\\CoreShift\\ui.json');
  }
  final home = Platform.environment['XDG_CONFIG_HOME'] ?? '${Platform.environment['HOME']}/.config';
  return File('$home/coreshift/ui.json');
}

/// UI-only preferences, such as the theme, kept per user.
Future<Map<String, dynamic>> loadPrefs() async {
  try {
    return jsonDecode(await _prefsFile().readAsString()) as Map<String, dynamic>;
  } catch (_) {
    return {};
  }
}

Future<void> savePrefs(Map<String, dynamic> prefs) async {
  try {
    final f = _prefsFile();
    await f.parent.create(recursive: true);
    await f.writeAsString(jsonEncode(prefs));
  } catch (_) {
    // Preferences are a convenience; losing them is not worth an error.
  }
}

/// The system, for the journal's header: "Windows 10 Pro 10.0 (Build 19045)".
/// Windows 11 still calls itself "Windows 10" there; its build number tells.
String get osDescription {
  var v = Platform.operatingSystemVersion;
  final build = int.tryParse(RegExp(r'Build (\d+)').firstMatch(v)?.group(1) ?? '');
  if (Platform.isWindows && build != null && build >= 22000) v = v.replaceFirst('Windows 10', 'Windows 11');
  return '${Platform.operatingSystem} $v';
}
