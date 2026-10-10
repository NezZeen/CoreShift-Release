import 'dart:io';

import 'package:coreshift/api/demo_backend.dart';
import 'package:coreshift/api/models.dart';
import 'package:coreshift/l10n/engine_strings.dart';
import 'package:coreshift/l10n/strings.dart';
import 'package:coreshift/l10n/ui_en.dart';
import 'package:coreshift/l10n/ui_ru.dart';
import 'package:coreshift/state/app_state.dart';
import 'package:coreshift/state/errors.dart';
import 'package:coreshift/ui/shell.dart' show mainPages;
import 'package:flutter_test/flutter_test.dart';

/// On once the whole UI is in the dictionary: then every Russian key must
/// have its English. Until then the missing ones are only listed.
const strictEnglish = false;

/// `"code": "template",` lines of a Go catalog (engine/internal/msg).
Map<String, String> goCatalog(String file) {
  final src = File('../engine/internal/msg/$file').readAsStringSync();
  final out = <String, String>{};
  for (final m in RegExp(r'^\s*(?:"([^"]+)"|CodeRaw):\s*"([^"]*)",', multiLine: true).allMatches(src)) {
    out[m[1] ?? 'raw'] = m[2]!;
  }
  return out;
}

List<String> argNames(String t) => ({for (final m in RegExp(r'\{([a-z_]+)[|}]').allMatches(t)) m[1]!}.toList()..sort());

void main() {
  tearDown(() => appLang.value = Lang.ru);

  group('dictionaries', () {
    test('the engine\'s Russian is catalog_ru.go to the letter', () {
      final go = goCatalog('catalog_ru.go');
      expect(go.length, greaterThan(150));
      expect(engineRu, go);
    });

    test('every engine code has its English, with the same arguments', () {
      for (final MapEntry(:key, :value) in engineRu.entries) {
        expect(engineEn.containsKey(key), isTrue, reason: key);
        expect(argNames(engineEn[key]!), argNames(value), reason: key);
      }
      expect(engineEn.keys.where((k) => !engineRu.containsKey(k)), isEmpty);
    });

    test('Android\'s notifications say what the app says (catalog_en.go)', () {
      final go = goCatalog('catalog_en.go');
      expect(go, isNotEmpty);
      for (final MapEntry(:key, :value) in go.entries) {
        expect(engineEn[key], value, reason: key);
      }
    });

    test('every key the UI uses is in the Russian dictionary', () {
      final used = <String>{};
      for (final f in Directory('lib').listSync(recursive: true).whereType<File>().where((f) => f.path.endsWith('.dart'))) {
        // The code, not the comments, which show keys as examples.
        final src = f.readAsLinesSync().where((l) => !l.trimLeft().startsWith('//')).join('\n');
        for (final m in RegExp(r"""\btr\(\s*'([a-z0-9_.\-]+)'""").allMatches(src)) {
          used.add(m[1]!);
        }
      }
      // Keys kept in lists and said with tr() where shown.
      for (final (_, _, key) in mainPages) {
        used.add(key);
      }
      expect(used, isNotEmpty);
      expect(used.where((k) => !uiRu.containsKey(k)), isEmpty);
    });

    test('the English UI dictionary', () {
      final missing = uiRu.keys.where((k) => !uiEn.containsKey(k)).toList();
      expect(uiEn.keys.where((k) => !uiRu.containsKey(k)), isEmpty, reason: 'English keys without Russian');
      for (final k in uiEn.keys) {
        expect(argNames(uiEn[k]!), argNames(uiRu[k]!), reason: k);
      }
      if (strictEnglish) {
        expect(missing, isEmpty);
      } else if (missing.isNotEmpty) {
        // ignore: avoid_print
        print('English lacks ${missing.length} keys: ${missing.take(10).join(', ')}…');
      }
    });
  });

  group('tr', () {
    test('says a key in the current language, Russian when English lacks it', () {
      expect(tr('nav.home'), 'Главная');
      appLang.value = Lang.en;
      expect(tr('nav.home'), 'Home');
      expect(tr('no.such.key'), 'no.such.key');
    });

    test('fills arguments and picks the word for a number', () {
      expect(fillTemplate('{n} {n|сервер|сервера|серверов}', {'n': 1}, Lang.ru, (v) => '$v'), '1 сервер');
      expect(fillTemplate('{n} {n|сервер|сервера|серверов}', {'n': 3}, Lang.ru, (v) => '$v'), '3 сервера');
      expect(fillTemplate('{n} {n|сервер|сервера|серверов}', {'n': 11}, Lang.ru, (v) => '$v'), '11 серверов');
      expect(fillTemplate('{n} {n|server|servers}', {'n': 1}, Lang.en, (v) => '$v'), '1 server');
      expect(fillTemplate('{n} {n|server|servers}', {'n': 5}, Lang.en, (v) => '$v'), '5 servers');
      expect(fillTemplate('Done. {more}', {}, Lang.en, (v) => '$v'), 'Done.');
    });

    test('the language: the choice in the settings, else the system\'s', () {
      expect(langFor('ru', systemLanguage: 'en', ready: true), Lang.ru);
      expect(langFor('en', systemLanguage: 'ru', ready: true), Lang.en);
      expect(langFor('system', systemLanguage: 'en', ready: true), Lang.en);
      expect(langFor(null, systemLanguage: 'de', ready: true), Lang.ru);
      // Until the English dictionary is complete, Russian whatever is chosen.
      expect(langFor('en', systemLanguage: 'en', ready: false), Lang.ru);
    });

    test('the settings keep the choice and note the language for the engine', () {
      final saved = <Json>[];
      final s = AppState(DemoBackend(), prefs: {}, savePrefs: (p) async => saved.add({...p}));
      s.applyLanguage(systemLanguage: 'en');
      expect(lang, englishReady ? Lang.en : Lang.ru);
      expect(s.prefs[langUsedPref], englishReady ? 'en' : 'ru');
      expect(s.languagePref, 'system');
      s.setLanguage('ru');
      expect(lang, Lang.ru);
      expect(s.prefs[langPref], 'ru');
      expect(saved.last[langUsedPref], 'ru');
      s.dispose();
    });
  });

  group('engine codes', () {
    test('a code renders in Russian and in English', () {
      const args = {'addr': '203.0.113.5:443'};
      expect(engineText('reach.server_up', args, 'fallback'), 'сервер 203.0.113.5:443 отвечает напрямую, но соединение через него не проходит');
      appLang.value = Lang.en;
      expect(engineText('reach.server_up', args, 'fallback'), 'server 203.0.113.5:443 answers directly, but nothing gets through it');
    });

    test('nested messages, lists and raw text', () {
      final args = {
        'errs': {
          'sep': '; ',
          'items': [
            {
              'code': 'net.host_err',
              'args': {
                'host': '1.1.1.1:443',
                'err': {'code': 'net.unreachable'},
              },
            },
            {
              'code': 'net.host_err',
              'args': {
                'host': '8.8.8.8:443',
                'err': {
                  'code': 'raw',
                  'args': {'text': 'i/o weird'},
                },
              },
            },
          ],
        },
      };
      expect(engineText('reach.offline', args, ''), 'не отвечают ни сервер, ни известные узлы: 1.1.1.1:443 — адрес недоступен; 8.8.8.8:443 — i/o weird');
      appLang.value = Lang.en;
      expect(
        engineText('reach.offline', args, ''),
        'neither the server nor well-known hosts answer: 1.1.1.1:443 — address unreachable; 8.8.8.8:443 — i/o weird',
      );
      expect(
        engineText('log.repeats', {
          'line': {
            'code': 'raw',
            'args': {'text': 'ERROR dial <адрес>'},
          },
          'n': 3,
          'period': {
            'code': 'time.sec',
            'args': {'n': 30},
          },
        }, ''),
        'ERROR dial <address> — 3 more times in 30 s',
      );
    });

    test('an unknown code, or one among the arguments, falls back to the engine\'s text', () {
      expect(engineText('from.a.newer.engine', {'x': 1}, 'текст службы'), 'текст службы');
      expect(
        engineText('reach.server_down', {
          'addr': 'a',
          'err': {'code': 'from.a.newer.engine'},
        }, 'текст службы'),
        'текст службы',
      );
      expect(engineText('', null, 'без кода'), 'без кода');
      appLang.value = Lang.en;
      expect(engineText('from.a.newer.engine', null, 'текст службы'), 'текст службы');
    });

    test('events and the status word their codes; raw ones stay as they are', () {
      final e = Event.fromJson({
        'kind': 'dns',
        'error': 'не удалось восстановить системный DNS: denied',
        'code': 'dns.revert_failed',
        'args': {
          'err': {
            'code': 'raw',
            'args': {'text': 'denied'},
          },
        },
      });
      final log = Event.fromJson({'kind': 'log', 'line': 'ERROR something'});
      final st = Status.fromJson({
        'state': 'failed',
        'error': 'адрес сервера vpn.example не найден в DNS: имя неверное или сервер убран, обновите подписку',
        'error_code': 'server.resolve.not_found',
        'error_args': {'host': 'vpn.example'},
      });
      expect(e.errorText, 'не удалось восстановить системный DNS: denied');
      expect(log.lineText, 'ERROR something');
      expect(humanCodedError(st.errorCode, st.errorArgs, st.error), startsWith('Адрес сервера vpn.example не найден в DNS'));
      appLang.value = Lang.en;
      expect(e.errorText, 'could not restore the system DNS: denied');
      expect(log.lineText, 'ERROR something');
      expect(
        humanCodedError(st.errorCode, st.errorArgs, st.error),
        'Server address vpn.example not found in DNS: the name is wrong or the server was removed; update the subscription.',
      );
      // An engine before the codes: its Russian, through humanError.
      expect(humanCodedError('', const {}, 'адрес не найден'), 'Адрес не найден.');
    });

    test('a checkup in English', () {
      appLang.value = Lang.en;
      final step = CheckStep.fromJson({
        'id': 'server',
        'title': 'Сервер',
        'status': 'ok',
        'detail': '«Польша»: адрес найден, порт отвечает (46 мс)',
        'code': 'checkup.server.ok',
        'args': {
          'who': {
            'code': 'checkup.who',
            'args': {'name': 'Польша'},
          },
          'what': {'code': 'checkup.server.port_ok'},
          'ms': 46,
        },
      });
      expect(step.title, 'Server');
      expect(step.detail, '“Польша”: address found, the port answers (46 ms)');
      final v = CheckVerdict.fromJson({
        'cause': 'server-down',
        'status': 'fail',
        'title': 'Сервер «Польша» не отвечает — выберите другой',
        'advice': 'Интернет работает, а сервер нет: он выключен или заблокирован.',
        'code': 'checkup.verdict.server_down',
        'args': {
          'srv': {
            'code': 'checkup.srv',
            'args': {'name': 'Польша'},
          },
        },
      });
      expect(v.title, 'Server “Польша” does not answer — pick another');
      expect(v.advice, 'The internet works, but the server does not: it is off or blocked.');
      // An engine before the codes.
      expect(CheckVerdict.fromJson({'title': 'Всё работает', 'advice': 'x'}).title, 'Всё работает');
    });

    test('the units of traffic', () {
      expect(formatQuota(100 * (1 << 30)), '100 ГБ');
      appLang.value = Lang.en;
      expect(formatQuota(100 * (1 << 30)), '100 GB');
    });
  });
}
