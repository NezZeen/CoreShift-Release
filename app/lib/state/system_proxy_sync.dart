// «Системный прокси»: while a connection without TUN is up, the proxy of the
// system points at CoreShift's port; otherwise it is what the user had. The
// app does it, as the user (engine/internal/sysproxy explains why), through
// `coreshiftd sysproxy`, which journals the user's settings first and
// leaves a guard and a sign-in entry behind in case the app or the service
// does not get to put them back.

/// What `coreshiftd sysproxy` reported.
class ProxyReport {
  final String action;
  final List<String> backends, applied, restored, changed, errors;
  final bool alive, pending;

  const ProxyReport({
    this.action = '',
    this.backends = const [],
    this.applied = const [],
    this.restored = const [],
    this.changed = const [],
    this.errors = const [],
    this.alive = false,
    this.pending = false,
  });

  factory ProxyReport.fromJson(Map<String, dynamic> j) {
    List<String> list(String k) => ((j[k] as List?) ?? const []).map((e) => '$e').toList();
    return ProxyReport(
      action: '${j['action'] ?? ''}',
      backends: list('backends'),
      applied: list('applied'),
      restored: list('restored'),
      changed: list('changed'),
      errors: list('errors'),
      alive: j['alive'] == true,
      pending: j['pending'] == true,
    );
  }
}

/// A desktop's name for the journal.
String proxyBackendName(String b) => switch (b) {
  'windows' => 'Windows',
  'gnome' => 'GNOME',
  'kde' => 'KDE',
  _ => b,
};

String _names(List<String> list) => list.map(proxyBackendName).join(', ');

/// The journal's lines for a report: (text, whether it is a warning).
List<(String, bool)> proxyReportLines(ProxyReport r, {required String address}) {
  final out = <(String, bool)>[];
  if (r.action == 'apply') {
    if (r.applied.isNotEmpty) {
      out.add(('системный прокси включён (${_names(r.applied)}): браузеры и программы идут через $address; прежние настройки сохранены', false));
    } else if (r.backends.isEmpty) {
      out.add((
        'системный прокси не настроен: рабочий стол не поддерживается (нужен GNOME или KDE). '
            'CoreShift работает как обычный прокси: укажите в программах SOCKS5 или HTTP $address',
        true,
      ));
      return out;
    }
  } else {
    if (r.restored.isNotEmpty) out.add(('системный прокси выключен, прежние настройки восстановлены (${_names(r.restored)})', false));
    if (r.changed.isNotEmpty) out.add(('системный прокси уже изменён вручную (${_names(r.changed)}): оставлен как есть', false));
  }
  for (final e in r.errors) {
    out.add(('системный прокси: $e', true));
  }
  if (r.action != 'apply' && r.pending && r.errors.isNotEmpty) {
    out.add(('прежние настройки прокси вернутся при следующем входе в систему или запуске CoreShift', true));
  }
  return out;
}

/// Keeps the proxy of the system in step with what the app wants: set
/// while [update] says so, put back otherwise. One call at a time; a
/// change while one runs is done after it, the latest wish only.
class SystemProxySync {
  /// Runs `coreshiftd sysproxy <args>` and returns its report.
  final Future<ProxyReport> Function(List<String> args) run;

  /// Whether a journal of settings to put back is there: left by an
  /// earlier run, it is restored when the app starts without wanting it.
  final bool Function() journalExists;

  /// Tells the journal what happened.
  final void Function(String text, bool warn) log;

  SystemProxySync({required this.run, required this.journalExists, required this.log});

  bool? _want;
  bool? _done;
  String _addr = '';
  bool _busy = false;

  /// The last call under way, for tests.
  Future<void>? pending;

  /// Says whether the proxy should be set now, to [address].
  void update(bool want, String address) {
    if (want == _want && (!want || address == _addr)) return;
    _want = want;
    _addr = address;
    if (!_busy) pending = _drain();
  }

  Future<void> _drain() async {
    _busy = true;
    try {
      var addr = '';
      while (_want != _done || (_want == true && addr != _addr)) {
        final want = _want!;
        addr = _addr;
        if (want) {
          final r = await run(['apply', '-addr', addr]);
          for (final (t, w) in proxyReportLines(r, address: addr)) {
            log(t, w);
          }
        } else if (_done == true || (_done == null && journalExists())) {
          final r = await run(['restore']);
          for (final (t, w) in proxyReportLines(r, address: addr)) {
            log(t, w);
          }
        }
        _done = want;
      }
    } finally {
      _busy = false;
    }
  }
}
