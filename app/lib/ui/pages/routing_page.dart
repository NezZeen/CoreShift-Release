import 'package:flutter/material.dart';

import '../../l10n/strings.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../shell.dart';
import '../theme.dart';
import '../widgets.dart';
import 'android_apps.dart';
import 'direct_apps.dart';
import 'rule_lists.dart';
import 'user_rules.dart';

class RoutingPage extends StatefulWidget {
  final AppState state;
  const RoutingPage({super.key, required this.state});

  @override
  State<RoutingPage> createState() => _RoutingPageState();
}

class _RoutingPageState extends State<RoutingPage> {
  AppState get s => widget.state;

  /// The list a phone shows: 'proxy', 'direct' or 'block'; null until chosen
  /// (then the one the mode is about).
  String? _tab;

  @override
  Widget build(BuildContext context) {
    if (isCompact(context)) return _phone(context);
    // An older daemon knows only the direct lists and would reject the rest.
    final full = s.hasSetting('routing.mode');
    final selected = full && s.setting('routing.mode', 'all') == 'selected';
    final tun = s.setting('tun', false);

    final List<Widget> left, right;
    if (selected) {
      left = [
        ServicePresetsPanel(state: s),
        RuleListPanel(
          key: const ValueKey('selected-proxy'),
          state: s,
          title: tr('rules.proxy.title'),
          sub: tr('rules.sub.domains'),
          description: tr('rules.proxy.desc'),
          domainsKey: 'routing.proxy_domains',
          ipsKey: 'routing.proxy_ips',
          hint: 'youtube.com, 91.108.4.0/22',
          empty: tr('rules.proxy.empty'),
          removeTip: tr('rules.proxy.remove'),
          chipColor: accent,
        ),
      ];
      right = [if (!platform.isAndroid) AppsPanel.proxy(state: s, description: tr('rules.proxy.apps.desc')), _blockPanel(), ..._ownRules()];
    } else {
      left = [
        if (s.hasSetting('routing.russia_direct')) RulePresets(state: s),
        RuleListPanel(
          key: const ValueKey('all-direct'),
          state: s,
          title: tr('rules.direct.title'),
          sub: tr('rules.sub.domains'),
          description: full ? tr('rules.direct.desc') : null,
          domainsKey: 'routing.direct_domains',
          ipsKey: 'routing.direct_ips',
          hint: full ? 'gosuslugi.ru, 10.8.0.0/16' : 'gosuslugi.ru, bank.example',
          empty: tr('rules.direct.empty'),
          removeTip: tr('rules.direct.remove'),
        ),
      ];
      // Programs are matched by their .exe; Android has no such thing.
      // The lists few people need, folded away.
      right = [
        if (s.hasSetting('routing.direct_apps') && !platform.isAndroid) AppsPanel.direct(state: s),
        if (full)
          Fold(
            title: tr('rules.always.title'),
            sub: tr('rules.always.sub'),
            child: RuleListPanel(
              key: const ValueKey('all-proxy'),
              bare: true,
              state: s,
              title: tr('rules.always.title'),
              description: tr('rules.always.desc'),
              domainsKey: 'routing.proxy_domains',
              ipsKey: 'routing.proxy_ips',
              hint: 'blocked.ru',
              empty: tr('rules.list.empty'),
              removeTip: tr('rules.list.remove'),
              chipColor: accent,
            ),
          ),
        if (full) _blockPanel(),
        ..._ownRules(),
      ];
    }

    // «Источник баз» is a row of its own under both columns.
    final geo = s.hasSetting('routing.geo')
        ? Fold(
            title: tr('rules.geo.fold'),
            sub: geoFoldSub(s),
            child: GeoSourcePanel(state: s),
          )
        : null;

    // Android picks the apps in the VPN before any rule: first.
    if (platform.isAndroid && s.hasSetting('routing.app_filter')) left.insert(0, AndroidAppsPanel(state: s));

    return PageFrame(
      children: [
        PageHeader(
          tr('rules.page.title'),
          subtitle: selected ? tr('rules.page.sub.selected') : tr('rules.page.sub.all'),
          // What always goes direct, for the curious: behind an icon.
          actions: [
            Tooltip(
              message: tr('rules.always_direct.tip'),
              child: InfoIcon(title: tr('rules.always_direct.title'), text: selected ? tr('rules.always_direct.selected') : tr('rules.always_direct.all')),
            ),
          ],
        ),
        _notice(context, tun),
        if (full) ...[_modePanel(context, selected), const SizedBox(height: 18)],
        LayoutBuilder(
          builder: (context, c) {
            List<Widget> spaced(List<Widget> ws) => [
              for (var i = 0; i < ws.length; i++) ...[if (i > 0) const SizedBox(height: 18), ws[i]],
            ];
            if (c.maxWidth < 900) return Column(children: spaced([...left, ...right, ?geo]));
            return Column(
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(child: Column(children: spaced(left))),
                    const SizedBox(width: 18),
                    Expanded(child: Column(children: spaced(right))),
                  ],
                ),
                if (geo != null) ...[const SizedBox(height: 18), geo],
              ],
            );
          },
        ),
      ],
    );
  }

  /// The proxy-only mode leaves the lists without effect: said here, with
  /// the way back. The mode itself is in the settings.
  Widget _notice(BuildContext context, bool tun) {
    final p = context.pal;
    if (tun) return const SizedBox();
    return Container(
      padding: const EdgeInsets.fromLTRB(14, 10, 10, 10),
      margin: const EdgeInsets.only(bottom: 18),
      decoration: BoxDecoration(
        color: warnColor.withValues(alpha: .07),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: warnColor.withValues(alpha: .3)),
      ),
      child: Row(
        children: [
          const Icon(Icons.info_outline, size: 17, color: warnColor),
          const SizedBox(width: 10),
          Expanded(
            child: Text.rich(
              TextSpan(
                style: TextStyle(fontSize: 13, color: p.muted),
                children: [
                  TextSpan(
                    text: '${tr('rules.notice.title')} ',
                    style: TextStyle(color: p.text, fontWeight: FontWeight.w600),
                  ),
                  TextSpan(text: tr('rules.notice.text')),
                ],
              ),
            ),
          ),
          if (s.info.tunAvailable) ...[
            const SizedBox(width: 10),
            Btn(label: tr('rules.notice.btn'), small: true, onPressed: () => s.updateSettings((x) => x['tun'] = true)),
          ],
        ],
      ),
    );
  }

  Widget _modePanel(BuildContext context, bool selected) {
    Widget option(String mode, IconData icon, String title, String text) {
      final on = (mode == 'selected') == selected;
      return Builder(
        builder: (context) {
          final p = context.pal;
          return InkWell(
            borderRadius: BorderRadius.circular(14),
            onTap: on ? null : () => s.updateSettings((x) => x['routing']['mode'] = mode),
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 150),
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: on ? Color.alphaBlend(accent.withValues(alpha: .1), p.surface) : p.surface,
                borderRadius: BorderRadius.circular(14),
                border: Border.all(color: on ? accent : p.border, width: on ? 2 : 1),
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(icon, size: 22, color: on ? p.accentInk : p.dim),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Flexible(child: Text(title, style: display(17))),
                            const SizedBox(width: 8),
                            if (on) Lamp(color: accent, lit: true, size: 7),
                          ],
                        ),
                        const SizedBox(height: 4),
                        Text(text, style: TextStyle(fontSize: 12, color: p.muted, height: 1.4)),
                      ],
                    ),
                  ),
                ],
              ),
            ),
          );
        },
      );
    }

    final allText = tr('rules.mode.all.text');
    final onlyText = tr('rules.mode.selected.text');
    if (isCompact(context)) {
      // A phone: a switch of two words and what the chosen mode does.
      return Builder(
        builder: (context) => Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Seg<bool>(
              value: selected,
              options: [(false, tr('rules.mode.all.title')), (true, tr('rules.mode.selected.title'))],
              onChanged: (v) => s.updateSettings((x) => x['routing']['mode'] = v ? 'selected' : 'all'),
            ),
            const SizedBox(height: 8),
            Text(selected ? onlyText : allText, style: TextStyle(fontSize: 12, color: context.pal.muted, height: 1.4)),
          ],
        ),
      );
    }
    final all = option('all', Icons.shield_outlined, tr('rules.mode.all.title'), allText);
    final only = option('selected', Icons.filter_alt_outlined, tr('rules.mode.selected.title'), onlyText);
    return LayoutBuilder(
      builder: (context, c) => c.maxWidth < 640
          ? Column(children: [all, const SizedBox(height: 10), only])
          : IntrinsicHeight(
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Expanded(child: all),
                  const SizedBox(width: 12),
                  Expanded(child: only),
                ],
              ),
            ),
    );
  }

  /// The user's own rules, folded away; the sources of their categories
  /// are a row of their own ([geo] in [build]).
  List<Widget> _ownRules() => [
    if (s.hasSetting('routing.rules'))
      Fold(
        title: tr('rules.own.title'),
        sub: tr('rules.own.sub'),
        child: UserRulesPanel(state: s, bare: true),
      ),
  ];

  int _count(List<String> keys) => keys.fold(0, (n, k) => n + s.setting<List>(k, const []).length);

  /// A phone: the mode, the programs, one list at a time with one field.
  Widget _phone(BuildContext context) {
    final full = s.hasSetting('routing.mode');
    final selected = full && s.setting('routing.mode', 'all') == 'selected';
    final tun = s.setting('tun', false);
    final p = context.pal;

    // The lists to choose from in this mode; an older daemon has the direct one only.
    final tabs = !full ? ['direct'] : (selected ? ['proxy', 'block'] : ['proxy', 'direct', 'block']);
    final tab = tabs.contains(_tab) ? _tab! : (selected || !full ? tabs.first : 'direct');
    final counts = {
      'proxy': _count(['routing.proxy_domains', 'routing.proxy_ips']),
      'direct': _count(['routing.direct_domains', 'routing.direct_ips']),
      'block': _count(['routing.block_domains']),
    };
    String tabLabel(String k) {
      final base = tr('rules.tab.$k');
      return counts[k]! > 0 ? '$base · ${counts[k]}' : base;
    }

    final list = switch (tab) {
      'proxy' => RuleListPanel(
        key: ValueKey('phone-proxy-$selected'),
        compact: true,
        quick: true,
        bare: true,
        state: s,
        title: tr(selected ? 'rules.proxy.title' : 'rules.always.title'),
        description: tr(selected ? 'rules.proxy.desc' : 'rules.always.desc'),
        domainsKey: 'routing.proxy_domains',
        ipsKey: 'routing.proxy_ips',
        hint: selected ? 'youtube.com, 91.108.4.0/22' : 'blocked.ru',
        empty: tr(selected ? 'rules.proxy.empty' : 'rules.list.empty'),
        removeTip: tr(selected ? 'rules.proxy.remove' : 'rules.list.remove'),
        chipColor: accent,
      ),
      'block' => RuleListPanel(
        key: const ValueKey('phone-block'),
        compact: true,
        bare: true,
        state: s,
        title: tr('rules.block.title'),
        description: tr('rules.block.desc'),
        domainsKey: 'routing.block_domains',
        hint: 'ads.example',
        empty: tr('rules.list.empty'),
        removeTip: tr('rules.block.remove'),
        chipColor: errColor,
      ),
      _ => RuleListPanel(
        key: const ValueKey('phone-direct'),
        compact: true,
        quick: full,
        bare: true,
        state: s,
        title: tr('rules.direct.title'),
        description: full ? tr('rules.direct.desc') : null,
        domainsKey: 'routing.direct_domains',
        ipsKey: 'routing.direct_ips',
        hint: full ? 'gosuslugi.ru, 10.8.0.0/16' : 'gosuslugi.ru, bank.example',
        empty: tr('rules.direct.empty'),
        removeTip: tr('rules.direct.remove'),
      ),
    };

    // Programs are matched by their .exe; Android picks the apps in the VPN.
    Widget? apps;
    if (platform.isAndroid && s.hasSetting('routing.app_filter')) {
      final mode = s.setting('routing.app_filter', 'all');
      final n = s.setting<List>('routing.filter_apps', const []).length;
      apps = _NavRow(
        title: tr('apps.android.title'),
        value: mode == 'all' ? tr('apps.android.all') : '$n',
        onTap: () => _push(context, tr('apps.android.title'), (_) => AndroidAppsPanel(state: s)),
      );
    } else if (!platform.isAndroid && (selected || s.hasSetting('routing.direct_apps'))) {
      final key = selected ? 'routing.proxy_apps' : 'routing.direct_apps';
      final n = s.setting<List>(key, const []).length;
      final title = tr(selected ? 'apps.proxy.title' : 'apps.direct.title');
      apps = _NavRow(
        title: title,
        value: n > 0 ? '$n' : null,
        onTap: () => _push(context, title, (_) => selected ? AppsPanel.proxy(state: s, description: tr('rules.proxy.apps.desc')) : AppsPanel.direct(state: s)),
      );
    }

    return PageFrame(
      children: [
        PageHeader(
          tr('rules.page.title'),
          actions: [
            Tooltip(
              message: tr('rules.always_direct.tip'),
              child: InfoIcon(title: tr('rules.always_direct.title'), text: selected ? tr('rules.always_direct.selected') : tr('rules.always_direct.all')),
            ),
          ],
        ),
        _notice(context, tun),
        if (full) ...[_modePanel(context, selected), const SizedBox(height: 14)],
        if (apps != null) ...[apps, const SizedBox(height: 14)],
        if (tabs.length > 1) ...[
          Seg<String>(value: tab, options: [for (final k in tabs) (k, tabLabel(k))], onChanged: (v) => setState(() => _tab = v)),
          const SizedBox(height: 14),
        ],
        list,
        const SizedBox(height: 18),
        Panel(
          dashed: true,
          onTap: () => Nav.to(context, PageId.settings),
          child: Row(
            children: [
              Icon(Icons.tune, size: 18, color: p.muted),
              const SizedBox(width: 10),
              Expanded(
                child: Text(tr('rules.hint.settings'), style: TextStyle(fontSize: 12, color: p.muted, height: 1.4)),
              ),
              Icon(Icons.chevron_right, size: 18, color: p.dim),
            ],
          ),
        ),
        if (s.hasSetting('routing.rules')) ...[const SizedBox(height: 14), ..._ownRules()],
      ],
    );
  }

  void _push(BuildContext context, String title, Widget Function(BuildContext) body) {
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => _RulesSubPage(title: title, state: s, body: body),
      ),
    );
  }

  Widget _blockPanel() => Fold(
    title: tr('rules.block.title'),
    sub: tr('rules.block.sub'),
    child: RuleListPanel(
      key: const ValueKey('block'),
      bare: true,
      state: s,
      title: tr('rules.block.title'),
      description: tr('rules.block.desc'),
      domainsKey: 'routing.block_domains',
      hint: 'ads.example',
      empty: tr('rules.list.empty'),
      removeTip: tr('rules.block.remove'),
      chipColor: errColor,
    ),
  );
}

/// The ready-made sets: Russian sites direct (in the «only selected» mode,
/// where they go direct anyway, the popular services to send through the
/// VPN instead). The rules page keeps them on a computer; a phone has them
/// in the settings, under «Подключение».
class RulePresets extends StatelessWidget {
  final AppState state;
  const RulePresets({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    final s = state;
    if (s.hasSetting('routing.mode') && s.setting('routing.mode', 'all') == 'selected') return ServicePresetsPanel(state: s);
    if (!s.hasSetting('routing.russia_direct')) return const SizedBox();
    final russiaOn = s.setting('routing.russia_direct', false);
    void set(String key, bool v) => s.updateSettings((x) => x['routing'][key] = v);
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle(tr('rules.presets.title')),
          SettingRow(
            first: true,
            title: tr('rules.presets.russia.title'),
            description: tr('rules.presets.russia.desc'),
            trailing: Switch(value: russiaOn, onChanged: (v) => set('russia_direct', v)),
          ),
          if (russiaOn && s.hasSetting('routing.russia_abroad'))
            SettingRow(
              title: tr('rules.presets.abroad.title'),
              description: tr('rules.presets.abroad.desc'),
              trailing: Switch(value: s.setting('routing.russia_abroad', false), onChanged: (v) => set('russia_abroad', v)),
            ),
          // The ad block has no switch for now (being reworked): it is on,
          // as the service keeps it (store: Routing.AdsVersion).
        ],
      ),
    );
  }
}

/// A line that opens a page of its own: the name, a count, a chevron.
class _NavRow extends StatelessWidget {
  final String title;
  final String? value;
  final VoidCallback onTap;
  const _NavRow({required this.title, this.value, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Material(
      color: p.surface,
      borderRadius: BorderRadius.circular(14),
      child: InkWell(
        borderRadius: BorderRadius.circular(14),
        onTap: onTap,
        child: Container(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(14),
            border: Border.all(color: p.border),
          ),
          child: Row(
            children: [
              Expanded(
                child: Text(title, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
              ),
              if (value != null) ...[Text(value!, style: TextStyle(fontSize: 13, color: p.muted)), const SizedBox(width: 6)],
              Icon(Icons.chevron_right, color: p.dim),
            ],
          ),
        ),
      ),
    );
  }
}

/// A page of the rules opened over the shell on a phone (the programs).
/// It listens to the state itself: the route is not rebuilt by the shell.
class _RulesSubPage extends StatelessWidget {
  final String title;
  final AppState state;
  final Widget Function(BuildContext) body;
  const _RulesSubPage({required this.title, required this.state, required this.body});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    // A window widened past the phone's layout: back to the page, which
    // shows every section itself there.
    if (!isCompact(context)) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (context.mounted) Navigator.of(context).maybePop();
      });
    }
    return Scaffold(
      backgroundColor: p.bg,
      body: SafeArea(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(4, 8, 16, 0),
              child: Row(
                children: [
                  IconButton(
                    icon: const Icon(Icons.arrow_back),
                    tooltip: MaterialLocalizations.of(context).backButtonTooltip,
                    onPressed: () => Navigator.pop(context),
                  ),
                  Expanded(
                    child: Text(title, style: display(22), overflow: TextOverflow.ellipsis),
                  ),
                ],
              ),
            ),
            Expanded(
              child: ListenableBuilder(
                listenable: state,
                builder: (context, _) => PageFrame(children: [body(context)]),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
