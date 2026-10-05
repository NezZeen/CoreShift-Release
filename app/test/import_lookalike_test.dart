import 'package:flutter_test/flutter_test.dart';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/state/app_state.dart';

/// The demo service with the real one's habit: subscriptions are listed with
/// their links masked (maskURL), the whole link only on request.
class _MaskingBackend extends DemoBackend {
  Json _masked(Json s) => {...s, 'url': maskedUrl('${s['url']}')};

  @override
  Future<dynamic> call(String method, String path, [Object? body]) async {
    final r = await super.call(method, path, body);
    if (method == 'GET' && path == '/v1/subscriptions') return [for (final s in r as List) _masked((s as Map).cast())];
    if (method == 'POST' && path == '/v1/subscriptions') return _masked((r as Map).cast());
    return r;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // Two links of one panel with short paths look alike without their
  // tokens ("http://10.0.2.2:28480/…"): the second used to be refused as
  // "already added" from a panel's link, a QR code or the clipboard.
  test('a link that only looks like an added one is offered', () async {
    final state = AppState(_MaskingBackend());
    state.start();
    for (var i = 0; i < 50 && !state.loaded; i++) {
      await Future.delayed(const Duration(milliseconds: 20));
    }
    expect(await state.addSubscription(source: 'http://panel.example.org/sub'), isNull);
    expect(state.subscriptions.last.url, 'http://panel.example.org/…');

    await state.offerImport('coreshift://add/http://panel.example.org/clash', ImportFrom.link);
    expect(state.pendingImport?.url, 'http://panel.example.org/clash');
    state.dismissImport();

    // The very link is still recognised.
    await state.offerImport('http://panel.example.org/sub', ImportFrom.qr);
    expect(state.pendingImport, isNull);
    expect(state.toasts.map((t) => t.message), contains('Эта подписка уже добавлена'));
    state.dispose();
  });
}
