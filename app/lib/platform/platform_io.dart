import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';

import '../api/backend.dart';
import '../api/models.dart';
import 'win_service.dart' as win;

/// On Android the engine runs inside the app; this channel reaches its
/// Kotlin side (MainActivity.kt).
const _android = MethodChannel('coreshift/android');

/// Where the Android engine keeps its API file; set by [initPlatform].
String? _androidApiFile;

bool get isAndroid => Platform.isAndroid;

/// What runs the VPN, for messages: a Windows service, or on Android the
/// engine inside the app.
String get _engine => Platform.isAndroid ? 'Движок CoreShift' : 'Служба CoreShift';
String get _ending => Platform.isAndroid ? '' : 'а';

/// Asks the platform what the app needs before it starts.
Future<void> initPlatform() async {
  if (Platform.isAndroid) _androidApiFile = await _android.invokeMethod<String>('apiFile');
}

/// Gets the user's consent to the VPN when Android needs it; false if the
/// user declined. Elsewhere the service has the rights it needs.
Future<bool> prepareVpn() async {
  if (!Platform.isAndroid) return true;
  return await _android.invokeMethod<bool>('prepareVpn') ?? false;
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
  return [
    for (final m in list) (package: m['package'] as String, label: m['label'] as String, system: m['system'] == true),
  ];
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

  Future<_Endpoint> _endpoint({bool reload = false}) async {
    if (_ep != null && !reload) return _ep!;
    final f = File(file);
    if (!await f.exists()) {
      throw DaemonOffline('$_engine не запущен$_ending');
    }
    try {
      final j = jsonDecode(await f.readAsString()) as Map<String, dynamic>;
      return _ep = _Endpoint(Uri.parse(j['address'] as String), j['token'] as String);
    } on FileSystemException catch (e) {
      throw DaemonOffline('Нет доступа к ${f.path}: ${e.osError?.message ?? e.message}');
    } catch (_) {
      throw DaemonOffline('Файл ${f.path} повреждён');
    }
  }

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    // A second attempt re-reads the API file: the daemon may have restarted
    // with a new port or token.
    for (var attempt = 0; ; attempt++) {
      final ep = await _endpoint(reload: attempt > 0);
      try {
        final req = await _client.openUrl(method, ep.base.resolve(path));
        req.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${ep.token}');
        if (body != null) {
          req.headers.contentType = ContentType.json;
          req.add(utf8.encode(jsonEncode(body)));
        }
        final resp = await req.close();
        final text = await resp.transform(utf8.decoder).join();
        if (resp.statusCode == HttpStatus.unauthorized && attempt == 0) continue;
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
          req = await _client.getUrl(ep.base.resolve('/v1/events?replay=1&app=1'));
          req!.headers.set(HttpHeaders.authorizationHeader, 'Bearer ${ep.token}');
          final resp = await req!.close();
          if (cancelled) return;
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
/// administrator rights; null where there is no such service.
bool Function()? get daemonStarter => Platform.isWindows ? win.startServiceQuietly : null;

/// Adds the app to the programs the system starts at sign-in, or removes
/// it; null where CoreShift does not (Android starts it at boot itself).
bool Function(bool on)? get autostartSetter => Platform.isWindows ? win.setAutostart : null;

/// Whether the UI can start a stopped service itself (with a UAC prompt).
bool get canStartService => Platform.isWindows;

/// Starts the CoreShift service, asking Windows for administrator rights.
/// Returns why it could not, or null once the request went through.
Future<String?> startService() async {
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

/// Runs the DNS leak test (see state/leak.dart) and returns bash.ws's
/// entries. The lookups go through the system resolver, the way every app's
/// do, so they take the tunnel's path when it is up.
Future<List<Map<String, dynamic>>> dnsLeakTest() async {
  final c = HttpClient()
    ..connectionTimeout = const Duration(seconds: 8)
    ..findProxy = ((_) => 'DIRECT')
    ..userAgent = 'CoreShift';
  Future<String> get(String url, {Duration timeout = const Duration(seconds: 15)}) async {
    final req = await c.getUrl(Uri.parse(url)).timeout(timeout);
    final resp = await req.close().timeout(timeout);
    final body = await resp.transform(utf8.decoder).join().timeout(timeout);
    if (resp.statusCode != HttpStatus.ok) throw HttpException('bash.ws: HTTP ${resp.statusCode}');
    return body;
  }

  try {
    final id = (await get('https://bash.ws/id')).trim();
    if (!RegExp(r'^[a-z0-9]{4,64}$').hasMatch(id)) throw const FormatException('bash.ws: unexpected test id');
    // Each name is unique, so no cache can answer it: some resolver has to
    // ask bash.ws. The requests themselves are expected to fail.
    await Future.wait([
      for (var i = 1; i <= 6; i++) get('http://$i.$id.bash.ws/', timeout: const Duration(seconds: 6)).then((_) => null, onError: (_) => null),
    ]);
    final list = jsonDecode(await get('https://bash.ws/dnsleak/test/$id?json')) as List;
    return [for (final e in list) (e as Map).cast<String, dynamic>()];
  } finally {
    c.close(force: true);
  }
}

/// The system, for the journal's header: "Windows 10 Pro 10.0 (Build 19045)".
String get osDescription => '${Platform.operatingSystem} ${Platform.operatingSystemVersion}';
