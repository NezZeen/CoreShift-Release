import 'dart:async';
import 'dart:convert';
import 'dart:io';

import '../api/backend.dart';
import '../api/models.dart';

Backend createBackend() => HttpBackend(apiFile());

/// The file where the daemon publishes its address and token.
String apiFile() {
  final override = Platform.environment['CORESHIFT_API_FILE'];
  if (override != null && override.isNotEmpty) return override;
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
      throw const DaemonOffline('Служба CoreShift не запущена');
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
        throw const DaemonOffline('Служба CoreShift не отвечает');
      } on HttpException {
        if (attempt == 0) continue;
        throw const DaemonOffline('Соединение со службой прервалось');
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
          req = await _client.getUrl(ep.base.resolve('/v1/events?replay=1'));
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
                onError: (Object e) => out.addError(const DaemonOffline('Соединение со службой прервалось')),
                onDone: out.close,
              );
        } catch (e) {
          if (cancelled) return;
          out.addError(e is SocketException || e is HttpException ? const DaemonOffline('Служба CoreShift не отвечает') : e);
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
