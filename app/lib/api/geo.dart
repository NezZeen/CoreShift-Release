import '../l10n/strings.dart';
import 'models.dart';

/// Базы категорий geosite и geoip, откуда берутся категории для своих правил
/// (engine/internal/service/geoapi.go): в каком состоянии каждая база и во что
/// превращается название, набранное пользователем.

/// Состояния базы.
const baseNone = 'none', baseIdle = 'idle', baseLoading = 'loading', baseLoaded = 'loaded', baseError = 'error';

/// Состояния правила с категорией.
const ruleOk = 'ok', ruleResolved = 'resolved', ruleWaiting = 'waiting', ruleMissing = 'missing', ruleAmbiguous = 'ambiguous', ruleError = 'error';

List<String> _strings(Object? v) => [for (final s in (v as List? ?? const [])) '$s'];

/// Одна база: geosite или geoip.
class GeoBase {
  final String kind;
  final String origin;
  final String state;

  /// Сколько уже скачано, процентов; -1, если размер неизвестен.
  final int percent;
  final int bytes;
  final int total;
  final DateTime? loadedAt;
  final int count;
  final String error;

  const GeoBase({
    this.kind = '',
    this.origin = '',
    this.state = baseIdle,
    this.percent = -1,
    this.bytes = 0,
    this.total = 0,
    this.loadedAt,
    this.count = 0,
    this.error = '',
  });

  factory GeoBase.fromJson(Json j) => GeoBase(
    kind: '${j['kind'] ?? ''}',
    origin: '${j['origin'] ?? ''}',
    state: '${j['state'] ?? baseIdle}',
    percent: (j['percent'] as num?)?.toInt() ?? -1,
    bytes: (j['bytes'] as num?)?.toInt() ?? 0,
    total: (j['total'] as num?)?.toInt() ?? 0,
    loadedAt: DateTime.tryParse('${j['loaded_at'] ?? ''}'),
    count: (j['count'] as num?)?.toInt() ?? 0,
    error: '${j['error'] ?? ''}',
  );

  bool get loading => state == baseLoading;
}

/// Правило с категорией и то, во что оно превращается в базе.
class GeoRuleState {
  final String match;
  final String state;

  /// Категория, которую правило будет использовать вместо набранной.
  final String name;
  final List<String> candidates;
  final List<String> suggestions;
  const GeoRuleState({required this.match, required this.state, this.name = '', this.candidates = const [], this.suggestions = const []});

  factory GeoRuleState.fromJson(Json j) => GeoRuleState(
    match: '${j['match'] ?? ''}',
    state: '${j['state'] ?? ''}',
    name: '${j['name'] ?? ''}',
    candidates: _strings(j['candidates']),
    suggestions: _strings(j['suggestions']),
  );
}

class GeoStatus {
  final List<GeoBase> bases;
  final List<GeoRuleState> rules;
  const GeoStatus({this.bases = const [], this.rules = const []});

  factory GeoStatus.fromJson(Json j) => GeoStatus(
    bases: [for (final b in (j['bases'] as List? ?? const [])) GeoBase.fromJson((b as Map).cast())],
    rules: [for (final r in (j['rules'] as List? ?? const [])) GeoRuleState.fromJson((r as Map).cast())],
  );

  GeoBase? base(String kind) {
    for (final b in bases) {
      if (b.kind == kind) return b;
    }
    return null;
  }

  GeoRuleState? rule(String match) {
    for (final r in rules) {
      if (r.match == match) return r;
    }
    return null;
  }

  /// Хоть что-то ещё грузится или ждёт загрузки: стоит спрашивать чаще.
  bool get busy => bases.any((b) => b.loading) || rules.any((r) => r.state == ruleWaiting);
}

/// Ответ на «что значит это название в базе».
class GeoCategories {
  final String kind;
  final String origin;
  final String state;

  /// База загружена, и остальное — о ней. [listed] false: источник не
  /// перечисляет категории (ссылка с {name}), проверить нечем.
  final bool loaded;
  final bool listed;
  final bool exact;

  /// Категория, которую возьмёт правило; пусто, если брать нечего.
  final String resolved;
  final List<String> ambiguous;
  final List<String> suggestions;
  final List<String> matches;
  const GeoCategories({
    this.kind = '',
    this.origin = '',
    this.state = baseIdle,
    this.loaded = false,
    this.listed = true,
    this.exact = false,
    this.resolved = '',
    this.ambiguous = const [],
    this.suggestions = const [],
    this.matches = const [],
  });

  factory GeoCategories.fromJson(Json j) => GeoCategories(
    kind: '${j['kind'] ?? ''}',
    origin: '${j['origin'] ?? ''}',
    state: '${j['state'] ?? baseIdle}',
    loaded: j['loaded'] == true,
    listed: j['listed'] != false,
    exact: j['exact'] == true,
    resolved: '${j['resolved'] ?? ''}',
    ambiguous: _strings(j['ambiguous']),
    suggestions: _strings(j['suggestions']),
    matches: _strings(j['matches']),
  );
}

/// Откуда база, словами.
String originName(String origin) => switch (origin) {
  'sagernet' => 'SagerNet',
  'runetfreedom' => 'runetfreedom',
  _ => tr('fmt.geo.origin.custom'),
};

/// Откуда база, названием: SagerNet, runetfreedom, своя ссылка.
String originTitle(String origin) => switch (origin) {
  'sagernet' => 'SagerNet',
  'runetfreedom' => 'runetfreedom',
  _ => tr('fmt.geo.originTitle.custom'),
};

/// «В базе …»: SagerNet, runetfreedom, своей ссылки.
String inBase(String origin) => tr('fmt.geo.inBase', {'origin': originName(origin)});

String _two(int n) => n.toString().padLeft(2, '0');

/// Когда загружена: «14:05», а если не сегодня — «12.10 в 14:05».
String loadedAtText(DateTime at, DateTime now) {
  final t = at.toLocal();
  final hm = '${_two(t.hour)}:${_two(t.minute)}';
  final n = now.toLocal();
  if (t.year == n.year && t.month == n.month && t.day == n.day) return hm;
  return tr('fmt.geo.loadedAtOtherDay', {'d': _two(t.day), 'mon': tr('fmt.month.${t.month}'), 'hm': hm});
}

/// Состояние базы одной строкой: «загружается… 42%», «загружена в 14:05».
String baseStateText(GeoBase b, DateTime now) => switch (b.state) {
  baseLoading => b.percent >= 0 ? tr('fmt.geo.state.loadingPercent', {'percent': b.percent}) : tr('fmt.geo.state.loading'),
  baseLoaded => tr('fmt.geo.state.loaded', {'at': loadedAtText(b.loadedAt ?? now, now)}),
  baseError => tr('fmt.geo.state.error', {'error': b.error.isEmpty ? tr('fmt.geo.state.errorDefault') : b.error}),
  baseNone => tr('fmt.geo.state.none'),
  _ => tr('fmt.geo.state.idle'),
};

/// Название базы в списке: «Сайты (geosite)».
String baseTitle(String kind) => kind == 'geoip' ? tr('fmt.geo.title.geoip') : tr('fmt.geo.title.geosite');
