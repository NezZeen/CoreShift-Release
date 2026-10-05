import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/backend.dart';
import 'package:coreshift/api/proof.dart';
import 'package:coreshift/platform/platform_io.dart';

/// A stand-in for the service on a free loopback port: it proves it knows
/// [token] unless [impostor], and records what it was asked.
class FakeService {
  final String token;
  final bool impostor;
  late final HttpServer server;
  final requests = <String>[];
  final bodies = <String>[];

  FakeService(this.token, {this.impostor = false});

  Future<void> start() async {
    server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    server.listen((req) async {
      requests.add('${req.method} ${req.uri.path}');
      final body = await utf8.decoder.bind(req).join();
      if (body.isNotEmpty) bodies.add(body);
      final nonce = req.headers.value(nonceHeader);
      if (!impostor && nonce != null) req.response.headers.set(proofHeader, apiProof(token, nonce));
      if (req.uri.path == '/v1/events') {
        req.response.headers.contentType = ContentType('text', 'event-stream');
        req.response.write('data: {"kind":"log","line":"hi","time":"2026-10-05T00:00:00Z"}\n\n');
        await req.response.flush();
        await req.response.close();
        return;
      }
      req.response.headers.contentType = ContentType.json;
      req.response.write(jsonEncode({'state': 'disconnected'}));
      await req.response.close();
    });
  }

  /// An api.json naming this server.
  Future<String> apiFile(Directory dir) async {
    final f = File('${dir.path}${Platform.pathSeparator}api.json');
    await f.writeAsString(jsonEncode({'address': 'http://127.0.0.1:${server.port}', 'token': token}));
    return f.path;
  }

  Future<void> stop() => server.close(force: true);
}

void main() {
  group('SHA-256 and HMAC', () {
    test('match the standard vectors', () {
      expect(hex(sha256([])), 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855');
      expect(hex(sha256(utf8.encode('abc'))), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
      expect(
        hex(sha256(utf8.encode('abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq'))),
        '248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1',
      );
      // RFC 4231, test cases 1, 2 and 6 (a key longer than a block).
      expect(hex(hmacSha256(List.filled(20, 0x0b), utf8.encode('Hi There'))), 'b0344c61d8db38535ca8afceaf0bf12b881dc200c9833da726e9376c2e32cff7');
      expect(
        hex(hmacSha256(utf8.encode('Jefe'), utf8.encode('what do ya want for nothing?'))),
        '5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843',
      );
      expect(
        hex(hmacSha256(List.filled(131, 0xaa), utf8.encode('Test Using Larger Than Block-Size Key - Hash Key First'))),
        '60e431591ee0b67f0d8a26aacbf5b77f8e0bc6213728c5140546040f0ee37f54',
      );
    });

    test('the proof is the one the service computes', () {
      // TestAPIProvesItKnowsTheToken in engine/internal/service.
      expect(apiProof('key', 'abc'), '60d75857ca50818b2d685ed7b41542c662fdb4b4716ba8c84aa43345918615e6');
      expect(proofValid(apiProof('key', 'abc'), 'key', 'abc'), isTrue);
      expect(proofValid(apiProof('key', 'abc').toUpperCase(), 'key', 'abc'), isTrue);
      expect(proofValid(apiProof('key', 'abd'), 'key', 'abc'), isFalse);
      expect(proofValid(apiProof('other', 'abc'), 'key', 'abc'), isFalse);
      expect(proofValid(null, 'key', 'abc'), isFalse);
      expect(proofValid('', 'key', 'abc'), isFalse);
      expect(newNonce(), isNot(newNonce()));
      expect(newNonce(), hasLength(32));
    });
  });

  group('HttpBackend', () {
    late Directory dir;
    setUp(() async => dir = await Directory.systemTemp.createTemp('coreshift-proof'));
    tearDown(() => dir.delete(recursive: true));

    test('talks to the service that proves it knows the token', () async {
      final svc = FakeService('the-token');
      await svc.start();
      addTearDown(svc.stop);
      final backend = HttpBackend(await svc.apiFile(dir));
      expect(await backend.call('GET', '/v1/status'), {'state': 'disconnected'});
      await backend.call('PUT', '/v1/selection', {'subscription': 'x'});
      expect(svc.requests, ['GET /v1/hello', 'GET /v1/status', 'PUT /v1/selection']);
      final events = await backend.events().toList();
      expect(events.single.line, 'hi');
    });

    test('sends nothing to whoever holds the port of a stale api.json', () async {
      final impostor = FakeService('stale-token', impostor: true);
      await impostor.start();
      addTearDown(impostor.stop);
      final backend = HttpBackend(await impostor.apiFile(dir));
      await expectLater(
        backend.call('POST', '/v1/subscriptions', {'url': 'https://panel.example/sub/secret'}),
        throwsA(isA<DaemonOffline>().having((e) => e.message, 'message', contains('другая программа'))),
      );
      expect(impostor.bodies, isEmpty);
      expect(impostor.requests.toSet(), {'GET /v1/hello'});
      await expectLater(backend.events().toList(), throwsA(isA<DaemonOffline>()));
      expect(impostor.requests.toSet(), {'GET /v1/hello'});
    });

    test('a stale api.json whose port nobody holds: the service is not running', () async {
      final gone = FakeService('t');
      await gone.start();
      final path = await gone.apiFile(dir);
      await gone.stop();
      await expectLater(HttpBackend(path).call('GET', '/v1/status'), throwsA(isA<DaemonOffline>()));
    });
  });
}
