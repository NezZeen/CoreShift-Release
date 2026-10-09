import 'package:flutter/material.dart';

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

  AppState get s => widget.state;

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
      onChanged: (_) => setState(() => _error = null),
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
            child: Text(
              ruleLabel(r['match']!),
              overflow: TextOverflow.ellipsis,
              style: const TextStyle(fontSize: 13, fontFamily: monoFont, fontFamilyFallback: monoFallback),
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
