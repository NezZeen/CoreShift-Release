import 'dart:async';

import 'package:flutter/material.dart';

import '../../api/geo.dart';
import '../../api/rules.dart';
import '../../state/app_state.dart';
import '../theme.dart';
import '../widgets.dart';

/// The user's own rules (routing.rules), in order: a category of sites
/// (geosite:), of addresses (geoip:), a site or an address, each through
/// the VPN, around it or blocked.
class UserRulesPanel extends StatefulWidget {
  final AppState state;
  const UserRulesPanel({super.key, required this.state});

  @override
  State<UserRulesPanel> createState() => _UserRulesPanelState();
}

class _UserRulesPanelState extends State<UserRulesPanel> {
  final _input = TextEditingController();
  String _action = 'proxy';
  String? _error;

  // What the base says of the name being typed (engine /v1/geo/categories),
  // for the text it was asked for.
  GeoCategories? _check;
  String _checked = '';
  Timer? _debounce;
  int _seq = 0;
  bool _rechecking = false;
  VoidCallback? _unwatch;

  AppState get s => widget.state;

  @override
  void initState() {
    super.initState();
    _unwatch = s.watchGeo();
  }

  /// "geosite:ua" gives (geosite, ua); null for anything else.
  ({String kind, String name})? _geoPart(String text) {
    final v = text.trim();
    final i = v.indexOf(':');
    if (i < 0) return null;
    final kind = v.substring(0, i).toLowerCase();
    final name = v.substring(i + 1).trim();
    if ((kind != 'geosite' && kind != 'geoip') || name.isEmpty) return null;
    return (kind: kind, name: name);
  }

  void _typed(String text) {
    setState(() => _error = null);
    _debounce?.cancel();
    _seq++;
    if (_geoPart(text) == null) {
      if (_check != null) setState(() => _check = null);
      return;
    }
    _debounce = Timer(const Duration(milliseconds: 250), () => _ask(text));
  }

  Future<void> _ask(String text) async {
    final part = _geoPart(text);
    if (part == null) return;
    final seq = ++_seq;
    final r = await s.geoCategories(part.kind, part.name);
    if (!mounted || seq != _seq) return;
    setState(() {
      _check = r;
      _checked = text;
    });
  }

  /// Puts the category into the field in place of the name typed.
  void _use(String kind, String name) {
    final text = '$kind:$name';
    _input.value = TextEditingValue(
      text: text,
      selection: TextSelection.collapsed(offset: text.length),
    );
    _typed(text);
  }

  List<Map<String, String>> get _rules => [
    for (final r in s.setting<List>('routing.rules', const []))
      if (r is Map) {'match': '${r['match'] ?? ''}', 'action': '${r['action'] ?? ''}'},
  ];

  Future<bool> _save(List<Map<String, String>> rules) async {
    final err = await s.updateSettings((x) => x['routing']['rules'] = rules);
    if (mounted) setState(() => _error = err);
    return err == null;
  }

  Future<void> _add() async {
    final parsed = parseRule(_input.text);
    if (parsed.error != null) {
      setState(() => _error = parsed.error);
      return;
    }
    final rules = _rules;
    final same = rules.indexWhere((r) => r['match'] == parsed.match);
    // Typed again with another action: the rule changes where it is.
    if (same >= 0) {
      rules[same] = {'match': parsed.match, 'action': _action};
    } else {
      rules.add({'match': parsed.match, 'action': _action});
    }
    if (await _save(rules)) {
      _input.clear();
      _typed('');
      s.toast(same >= 0 ? 'Правило изменено' : 'Правило добавлено', ToastKind.ok);
    }
  }

  Future<void> _move(int i, int by) async {
    final rules = _rules;
    final j = i + by;
    if (j < 0 || j >= rules.length) return;
    final r = rules.removeAt(i);
    rules.insert(j, r);
    await _save(rules);
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _unwatch?.call();
    _input.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final rules = _rules;
    final field = TextField(
      controller: _input,
      onSubmitted: (_) => _add(),
      onChanged: _typed,
      style: const TextStyle(fontSize: 13, fontFamily: monoFont, fontFamilyFallback: monoFallback),
      decoration: const InputDecoration(hintText: 'geosite:youtube, geoip:ru, example.com'),
    );
    final pick = Seg<String>(options: ruleActions, value: _action, onChanged: (v) => setState(() => _action = v));
    final add = Btn(label: 'Добавить', icon: Icons.add, onPressed: _add);
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle(
            'Свои правила',
            sub: 'сверху вниз',
            info:
                'Правила проверяются по порядку: первое подходящее решает. Они важнее готовых наборов, '
                'но ваши списки сайтов «без VPN», «через VPN» и «Блокировать» важнее правил. '
                'Правила geoip срабатывают для сайтов, которых нет ни в одном правиле по имени.',
          ),
          Text(
            'Категория сайтов, адресов, сайт или подсеть. Например: geosite:category-ads-all — блокировать, '
            'geosite:youtube — через VPN, geoip:ru — напрямую.',
            style: TextStyle(fontSize: 12, color: p.muted, height: 1.45),
          ),
          const SizedBox(height: 12),
          LayoutBuilder(
            builder: (context, c) => c.maxWidth < 760
                ? Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      field,
                      const SizedBox(height: 8),
                      Row(
                        children: [
                          Expanded(
                            child: Align(alignment: Alignment.centerLeft, child: pick),
                          ),
                          const SizedBox(width: 8),
                          add,
                        ],
                      ),
                    ],
                  )
                : Row(
                    children: [
                      Expanded(child: field),
                      const SizedBox(width: 8),
                      pick,
                      const SizedBox(width: 8),
                      add,
                    ],
                  ),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(_error!, style: const TextStyle(color: errColor, fontSize: 12)),
            ),
          ..._checkWidgets(context),
          GeoBasesNote(state: s),
          const SizedBox(height: 12),
          if (rules.isEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 14),
              child: Center(
                child: Text('Пока нет своих правил', style: TextStyle(color: p.dim, fontSize: 12)),
              ),
            )
          else
            for (var i = 0; i < rules.length; i++) _row(context, rules, i),
        ],
      ),
    );
  }

  /// Under the field: what the base says of the name typed, and the names to
  /// pick from while typing.
  List<Widget> _checkWidgets(BuildContext context) {
    final p = context.pal;
    final part = _geoPart(_input.text);
    final c = _check;
    if (part == null || c == null || _checked != _input.text) return const [];
    // A base that came after the question: ask again.
    final base = s.geo.base(part.kind);
    if (!c.loaded && c.listed && base != null && base.state == baseLoaded && !_rechecking) {
      _rechecking = true;
      WidgetsBinding.instance.addPostFrameCallback((_) async {
        if (mounted) await _ask(_input.text);
        _rechecking = false;
      });
    }
    Widget line(Key key, List<InlineSpan> spans, {Color? color}) => Padding(
      padding: const EdgeInsets.only(top: 8),
      child: Text.rich(
        TextSpan(
          style: TextStyle(fontSize: 12, color: color ?? p.muted, height: 1.4),
          children: spans,
        ),
        key: key,
      ),
    );
    Widget pick(String name) => GeoPick(name: name, onTap: () => _use(part.kind, name));
    InlineSpan chip(String name) => WidgetSpan(
      alignment: PlaceholderAlignment.middle,
      child: Padding(padding: const EdgeInsets.only(right: 4), child: pick(name)),
    );
    final out = <Widget>[];
    if (!c.listed) return const [];
    if (!c.loaded) {
      final text = switch (c.state) {
        baseError => 'База не загрузилась: категорию проверить нельзя.',
        baseIdle => 'База ещё не загружена.',
        _ => base != null && base.percent >= 0 ? 'База ещё загружается… ${base.percent}%' : 'База ещё загружается…',
      };
      return [
        line(const ValueKey('geo-note-loading'), [TextSpan(text: text)]),
      ];
    }
    if (c.resolved.isNotEmpty && c.exact) {
      out.add(line(const ValueKey('geo-note-ok'), [TextSpan(text: 'Есть ${inBase(c.origin)}.')], color: okColor));
    } else if (c.resolved.isNotEmpty) {
      out.add(line(const ValueKey('geo-note-resolved'), [const TextSpan(text: 'Будет использована '), chip(c.resolved)], color: okColor));
    } else if (c.ambiguous.isNotEmpty) {
      out.add(
        line(const ValueKey('geo-note-ambiguous'), [
          const TextSpan(text: 'Подходит несколько категорий, выберите: '),
          for (final n in c.ambiguous) chip(n),
        ], color: warnColor),
      );
    } else if (c.suggestions.isNotEmpty) {
      out.add(
        line(const ValueKey('geo-note-typo'), [
          const TextSpan(text: 'Нет в базе. Может быть, '),
          for (final n in c.suggestions) chip(n),
          const TextSpan(text: '?'),
        ], color: warnColor),
      );
    } else {
      out.add(line(const ValueKey('geo-note-missing'), [TextSpan(text: 'Нет такой категории ${inBase(c.origin)}.')], color: errColor));
    }
    // Names to pick while typing, unless the one typed is all there is.
    final more = c.matches.where((m) => m != c.resolved || c.matches.length > 1).toList();
    if (more.isNotEmpty && !(c.exact && c.matches.length == 1)) {
      out.add(
        Padding(
          padding: const EdgeInsets.only(top: 8),
          child: Wrap(key: const ValueKey('geo-matches'), spacing: 6, runSpacing: 6, children: [for (final m in c.matches) pick(m)]),
        ),
      );
    }
    return out;
  }

  /// What the base made of a rule of a category, under its name: waiting for
  /// the base, no such category (with the names that may be meant), the name
  /// another spelling of a category.
  Widget? _ruleNote(BuildContext context, List<Map<String, String>> rules, int i) {
    final p = context.pal;
    final r = rules[i];
    final st = s.geo.rule(r['match']!);
    if (st == null) return null;
    final kind = r['match']!.startsWith('geoip:') ? 'geoip' : 'geosite';
    Widget fix(String name) => GeoPick(name: name, onTap: () => _save([...rules]..[i] = {'match': '$kind:$name', 'action': r['action']!}));
    final small = TextStyle(fontSize: 11, color: p.dim, height: 1.3);
    switch (st.state) {
      case ruleWaiting:
        return const Padding(
          padding: EdgeInsets.only(top: 3),
          child: Pill('ждёт базу', color: warnColor),
        );
      case ruleResolved:
        return Padding(
          padding: const EdgeInsets.only(top: 2),
          child: Text('→ ${st.name}', style: small),
        );
      case ruleMissing:
        return Padding(
          padding: const EdgeInsets.only(top: 3),
          child: Wrap(
            spacing: 4,
            runSpacing: 4,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              const Pill('нет в базе', color: errColor),
              if (st.suggestions.isNotEmpty) Text('Может быть:', style: small),
              for (final n in st.suggestions) fix(n),
            ],
          ),
        );
      case ruleAmbiguous:
        return Padding(
          padding: const EdgeInsets.only(top: 3),
          child: Wrap(
            spacing: 4,
            runSpacing: 4,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              const Pill('неоднозначно', color: warnColor),
              Text('Выберите:', style: small),
              for (final n in st.candidates) fix(n),
            ],
          ),
        );
      case ruleError:
        return const Padding(
          padding: EdgeInsets.only(top: 3),
          child: Pill('база не загрузилась', color: errColor),
        );
    }
    return null;
  }

  Widget _row(BuildContext context, List<Map<String, String>> rules, int i) {
    final p = context.pal;
    final r = rules[i];
    final color = switch (r['action']) {
      'block' => errColor,
      'direct' => okColor,
      _ => accent,
    };
    Widget icon(IconData icon, String tip, VoidCallback? onTap) => Tooltip(
      message: tip,
      child: InkWell(
        borderRadius: BorderRadius.circular(6),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.all(5),
          child: Icon(icon, size: 16, color: onTap == null ? p.border : p.dim),
        ),
      ),
    );
    return Container(
      key: ValueKey('rule-${r['match']}'),
      margin: const EdgeInsets.only(bottom: 6),
      padding: const EdgeInsets.fromLTRB(10, 4, 4, 4),
      decoration: BoxDecoration(
        color: p.surface2,
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: p.border),
      ),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  ruleLabel(r['match']!),
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontSize: 13, fontFamily: monoFont, fontFamilyFallback: monoFallback),
                ),
                ?_ruleNote(context, rules, i),
              ],
            ),
          ),
          const SizedBox(width: 8),
          Pill(ruleActionName(r['action']!), color: color),
          const SizedBox(width: 4),
          icon(Icons.arrow_upward, 'Выше', i > 0 ? () => _move(i, -1) : null),
          icon(Icons.arrow_downward, 'Ниже', i < rules.length - 1 ? () => _move(i, 1) : null),
          icon(Icons.close, 'Удалить правило', () => _save([...rules]..removeAt(i))),
        ],
      ),
    );
  }
}

/// A category to pick: a name in a small frame, tapped to use it.
class GeoPick extends StatelessWidget {
  final String name;
  final VoidCallback onTap;
  const GeoPick({super.key, required this.name, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return InkWell(
      borderRadius: BorderRadius.circular(6),
      onTap: onTap,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
        decoration: BoxDecoration(
          color: accent.withValues(alpha: .1),
          borderRadius: BorderRadius.circular(6),
          border: Border.all(color: accent.withValues(alpha: .4)),
        ),
        child: Text(
          name,
          style: TextStyle(fontSize: 12, color: p.text, fontFamily: monoFont, fontFamilyFallback: monoFallback),
        ),
      ),
    );
  }
}

/// One base: its source, what it is doing and a way to try again. A
/// download shows how far it is, a bar without an end when the size is not
/// known.
class GeoBaseRow extends StatelessWidget {
  final AppState state;
  final GeoBase base;
  const GeoBaseRow({super.key, required this.state, required this.base});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final b = base;
    final color = switch (b.state) {
      baseError => errColor,
      baseLoaded => okColor,
      baseLoading => accent,
      _ => p.muted,
    };
    final action = switch (b.state) {
      baseError => 'Повторить',
      baseIdle => 'Загрузить',
      _ => null,
    };
    return Padding(
      key: ValueKey('geo-base-${b.kind}'),
      padding: const EdgeInsets.only(top: 8),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(
                child: Text.rich(
                  TextSpan(
                    style: TextStyle(fontSize: 12, color: p.muted, height: 1.4),
                    children: [
                      TextSpan(
                        text: '${baseTitle(b.kind)} · ${originTitle(b.origin)}',
                        style: TextStyle(color: p.text, fontWeight: FontWeight.w600),
                      ),
                      const TextSpan(text: '  '),
                      TextSpan(
                        text: baseStateText(b, DateTime.now()),
                        style: TextStyle(color: p.ink(color)),
                      ),
                      if (b.state == baseLoaded && b.count > 0) TextSpan(text: ' · ${b.count} ${ruPlural(b.count, 'категория', 'категории', 'категорий')}'),
                    ],
                  ),
                  key: ValueKey('geo-state-${b.kind}'),
                  maxLines: 4,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              if (action != null) ...[
                const SizedBox(width: 8),
                Btn(key: ValueKey('geo-retry-${b.kind}'), label: action, small: true, onPressed: () => state.reloadGeo(b.kind)),
              ],
            ],
          ),
          if (b.state == baseLoading)
            Padding(
              padding: const EdgeInsets.only(top: 5),
              child: ClipRRect(
                borderRadius: BorderRadius.circular(3),
                child: LinearProgressIndicator(
                  key: ValueKey('geo-progress-${b.kind}'),
                  value: b.percent >= 0 ? b.percent / 100 : null,
                  minHeight: 4,
                  color: accent,
                  backgroundColor: p.border,
                ),
              ),
            ),
        ],
      ),
    );
  }
}

/// Above the rules, while a base is downloading or did not download: it is
/// seen without opening the source of the bases.
class GeoBasesNote extends StatelessWidget {
  final AppState state;
  const GeoBasesNote({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    final shown = [
      for (final b in state.geo.bases)
        if (b.state == baseLoading || b.state == baseError) b,
    ];
    if (shown.isEmpty) return const SizedBox();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [for (final b in shown) GeoBaseRow(state: state, base: b)],
    );
  }
}

/// What the closed «Источник баз» says of itself: the source, and that a
/// base is downloading or did not download.
String geoFoldSub(AppState s) {
  final name = geoSources.firstWhere((g) => g.$1 == s.setting('routing.geo.source', 'sagernet'), orElse: () => geoSources.first).$2;
  if (s.geo.bases.any((b) => b.loading)) return '$name · загружается…';
  if (s.geo.bases.any((b) => b.state == baseError)) return '$name · ошибка загрузки';
  return name;
}

/// Where the categories of the rules come from (routing.geo), and whether
/// the ready-made sets come from there too.
class GeoSourcePanel extends StatelessWidget {
  final AppState state;
  const GeoSourcePanel({super.key, required this.state});

  AppState get s => state;

  Future<String?> _set(String key, Object value) => s.updateSettings((x) {
    final r = x['routing'] as Map;
    final g = Map<String, dynamic>.from(r['geo'] as Map? ?? const {});
    g[key] = value;
    r['geo'] = g;
  });

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final source = s.setting('routing.geo.source', 'sagernet');
    final about = switch (source) {
      'runetfreedom' =>
        'Списки runetfreedom (russia-blocked-geosite): ru-blocked — сайты, заблокированные в России, category-ads-all — реклама, '
            'и категории v2fly. Это один файл geosite.dat на десятки мегабайт; категории geoip берутся у SagerNet.',
      'custom' =>
        'Ссылка https с {name} на файлы .srs, например https://example.org/geosite-{name}.srs, '
            'или ссылка на geosite.dat / geoip.dat в формате v2ray. Пустое поле — эти категории у SagerNet.',
      _ => 'Категории SagerNet (sing-geosite, sing-geoip): geosite:youtube, geosite:category-ads-all, geoip:ru и другие.',
    };
    Widget link(String key, String label, String hint) => Padding(
      padding: const EdgeInsets.only(top: 10),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(label, style: TextStyle(fontSize: 12, color: p.muted)),
          const SizedBox(height: 4),
          SavingField(
            key: ValueKey('geo-$key'),
            value: s.setting('routing.geo.$key', ''),
            width: double.infinity,
            mono: true,
            hint: hint,
            onSave: (v) async {
              final err = sourceLinkError(v);
              if (err != null) {
                s.toast(err, ToastKind.err);
                return err;
              }
              return _set(key, v.trim());
            },
          ),
        ],
      ),
    );
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('Откуда берутся категории geosite: и geoip: для своих правил.', style: TextStyle(fontSize: 12, color: p.muted, height: 1.45)),
        const SizedBox(height: 10),
        Align(
          alignment: Alignment.centerLeft,
          child: Seg<String>(options: geoSources, value: source, onChanged: (v) => _set('source', v)),
        ),
        const SizedBox(height: 8),
        Text(about, style: TextStyle(fontSize: 12, color: p.muted, height: 1.45)),
        for (final b in s.geo.bases)
          if (b.state != baseNone) GeoBaseRow(state: s, base: b),
        if (source == 'custom') ...[
          link('geosite_url', 'Категории сайтов (geosite)', 'https://example.org/geosite.dat'),
          link('geoip_url', 'Категории адресов (geoip)', 'https://example.org/geoip-{name}.srs'),
        ],
        if (source != 'sagernet') ...[
          SettingRow(
            title: 'Готовые наборы — тоже отсюда',
            description:
                'Списки «Российские сайты напрямую» и блокировки рекламы возьмутся из этого источника. '
                'Если нужной категории там нет, работает встроенная копия.',
            trailing: Switch(value: s.setting('routing.geo.presets', false), onChanged: (v) => _set('presets', v)),
          ),
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Row(
              children: [
                const Icon(Icons.info_outline, size: 14, color: warnColor),
                const SizedBox(width: 6),
                Expanded(
                  child: Text(
                    'Списки ведёт сторонний источник: CoreShift проверяет только их формат и размер.',
                    style: TextStyle(fontSize: 12, color: p.muted),
                  ),
                ),
              ],
            ),
          ),
        ],
      ],
    );
  }
}
