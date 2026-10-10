part of '../app_state.dart';

/// The steps of «Проверить всё» in the order the service runs and shows
/// them (engine/internal/service/checkup.go), with their names until it
/// sends its own.
const checkupSteps = [
  ('network', 'Сеть устройства'),
  ('internet', 'Интернет напрямую'),
  ('dns', 'DNS сети'),
  ('server', 'Сервер'),
  ('tunnel', 'Связь через VPN'),
  ('tunnel-dns', 'DNS через VPN'),
  ('leak', 'Утечка DNS'),
  ('speed', 'Скорость'),
  ('direct', 'Прямые соединения'),
];

/// A step's name in the app's language; [fallback] for one it does not know.
String checkupStepTitle(String id, String fallback) => engineText('checkup.step.$id', null, fallback.isEmpty ? id : fallback);

/// One step of a checkup: [status] is "ok", "warn", "fail" or "skipped",
/// "" while it runs.
class CheckStep {
  final String id;
  final String title;
  final String status;
  final String detail;
  final int latencyMs;
  final int downloadBps;
  const CheckStep({required this.id, required this.title, this.status = '', this.detail = '', this.latencyMs = 0, this.downloadBps = 0});

  bool get running => status.isEmpty;

  /// The title and the detail in the app's language: the engine's codes
  /// (l10n/engine_strings.dart), else its Russian.
  factory CheckStep.fromJson(Json j) => CheckStep(
    id: j['id'] ?? '',
    title: checkupStepTitle(j['id'] ?? '', j['title'] ?? ''),
    status: j['status'] ?? '',
    detail: engineText(j['code'] ?? '', j['args'], j['detail'] ?? ''),
    latencyMs: (j['latency_ms'] as num?)?.toInt() ?? 0,
    downloadBps: (j['download_bps'] as num?)?.toInt() ?? 0,
  );

  /// The step as the report for support shows it: ✓, ! or ✗, and – for
  /// one that did not run.
  String get mark => switch (status) {
    'ok' => '✓',
    'warn' => '!',
    'fail' => '✗',
    _ => '–',
  };
}

/// What a checkup found: the likely [cause], what to do, and the actions
/// the app offers for it ("servers", "reconnect", "connect", "routing",
/// "leak"), the first the main one.
class CheckVerdict {
  final String cause;
  final String status;
  final String title;
  final String advice;
  final List<String> actions;
  const CheckVerdict({this.cause = '', this.status = '', this.title = '', this.advice = '', this.actions = const []});

  /// The words in the app's language: the code with ".title" and
  /// ".advice", else the engine's Russian.
  factory CheckVerdict.fromJson(Json j) => CheckVerdict(
    cause: j['cause'] ?? '',
    status: j['status'] ?? '',
    title: (j['code'] ?? '') == '' ? j['title'] ?? '' : engineText('${j['code']}.title', j['args'], j['title'] ?? ''),
    advice: (j['code'] ?? '') == '' ? j['advice'] ?? '' : engineText('${j['code']}.advice', j['args'], j['advice'] ?? ''),
    actions: [for (final a in j['actions'] as List? ?? const []) '$a'],
  );
}

/// «Проверить всё» as the app shows it: the steps as they finish, then the
/// verdict; or why it could not run.
class CheckupState {
  final bool running;
  final List<CheckStep> steps;
  final CheckVerdict? verdict;
  final String error;

  /// The server checked, by its display name.
  final String server;

  /// When the result came.
  final DateTime? at;
  const CheckupState({this.running = false, this.steps = const [], this.verdict, this.error = '', this.server = '', this.at});

  bool get done => verdict != null;
}

/// The checkup runs in the service (POST /v1/checkup), which reports each
/// step as a "checkup" event as it finishes.
extension AppStateCheckup on AppState {
  /// Runs every check at once; the result lands in [checkup] and, a few
  /// lines, in the journal.
  Future<void> runCheckup() async {
    if (checkup.running) return;
    if (!online) {
      checkup = CheckupState(
        error: 'Служба CoreShift не отвечает${offlineReason.isEmpty ? '' : ': $offlineReason'}. Перезапустите CoreShift; если не поможет — переустановите его.',
        at: DateTime.now(),
      );
      _log(DateTime.now(), 'диагностика', 'проверка не запустилась: служба CoreShift не отвечает', LogLevel.err);
      _notify();
      return;
    }
    checkup = CheckupState(
      running: true,
      steps: [for (final (id, title) in checkupSteps) CheckStep(id: id, title: checkupStepTitle(id, title))],
    );
    _notify();
    try {
      final r = await backend.call('POST', '/v1/checkup', {'speed': true}) as Json;
      final steps = [for (final s in r['steps'] as List? ?? const []) CheckStep.fromJson((s as Map).cast())];
      checkup = CheckupState(
        steps: steps,
        verdict: CheckVerdict.fromJson((r['verdict'] as Map? ?? const {}).cast()),
        server: r['server'] ?? '',
        at: DateTime.now(),
      );
      _logCheckup();
    } catch (e) {
      final text = switch (e) {
        ApiError(status: 404 || 405) => 'Служба CoreShift старой версии и не умеет так проверять. Обновите CoreShift.',
        ApiError(status: 409) => 'Проверка уже идёт',
        DaemonOffline(:final message) => message,
        _ => 'Проверка не удалась: ${humanError('$e')}',
      };
      checkup = CheckupState(error: text, at: DateTime.now());
      _log(DateTime.now(), 'диагностика', 'проверка не удалась: $e', LogLevel.err);
      if (e is DaemonOffline) _lost(e);
    }
    _notify();
  }

  /// A step finished: it shows at once, before the whole result comes.
  void _onCheckupEvent(Event e) {
    if (!checkup.running || e.reason != 'step' || e.step.isEmpty) return;
    final steps = [...checkup.steps];
    final i = steps.indexWhere((s) => s.id == e.step);
    final title = i >= 0 ? steps[i].title : e.step;
    final step = CheckStep(id: e.step, title: title, status: e.status, detail: e.lineText, latencyMs: e.latencyMs);
    if (i >= 0) {
      steps[i] = step;
    } else {
      steps.add(step);
    }
    checkup = CheckupState(running: true, steps: steps);
    _notify();
  }

  /// The result in the journal: the verdict, every step in a line, and what
  /// was wrong, each in its own.
  void _logCheckup() {
    final c = checkup, v = c.verdict;
    if (v == null) return;
    final t = DateTime.now();
    final level = switch (v.status) {
      'ok' => LogLevel.ok,
      'warn' => LogLevel.warn,
      _ => LogLevel.err,
    };
    _log(t, 'диагностика', 'итог: ${v.title}', level);
    _log(t, 'диагностика', c.steps.map((s) => '${s.title} ${s.mark}').join(' · '), LogLevel.info);
    for (final s in c.steps.where((s) => s.status == 'warn' || s.status == 'fail')) {
      _log(t, 'диагностика', '${s.title}: ${s.detail}', s.status == 'fail' ? LogLevel.err : LogLevel.warn);
    }
  }

  /// The checkup for support, before the journal: when, the verdict and
  /// every step. Servers only by name, links cut as in the journal.
  List<String> checkupReport() {
    final c = checkup;
    final t = c.at ?? DateTime.now();
    String two(int n) => n.toString().padLeft(2, '0');
    final v = c.verdict;
    return [
      'Проверка CoreShift: ${t.year}-${two(t.month)}-${two(t.day)} ${two(t.hour)}:${two(t.minute)}:${two(t.second)}',
      if (v != null) ...['Итог: ${v.title}', v.advice] else if (c.error.isNotEmpty) 'Итог: ${c.error}',
      for (final s in c.steps) '  ${s.mark} ${s.title} — ${s.running ? 'не закончено' : s.detail}',
      '',
    ].map(redactForSupport).toList();
  }

  /// «Отправить в поддержку»: the report and the journal after it, on the
  /// clipboard and as a file, as «Копировать» in the journal saves it.
  Future<String?> sendCheckup() => copyJournal(before: checkupReport(), kind: 'check');
}
