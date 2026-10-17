import 'package:flutter/material.dart';

import '../../l10n/strings.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../theme.dart';
import '../widgets.dart';
import 'android_apps.dart';
import 'direct_apps.dart';
import 'rule_lists.dart';
import 'user_rules.dart';

class RoutingPage extends StatelessWidget {
  final AppState state;
  const RoutingPage({super.key, required this.state});

  AppState get s => state;

  @override
  Widget build(BuildContext context) {
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
        if (s.hasSetting('routing.russia_direct')) _presetsPanel(context, selected: false),
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
            if (c.maxWidth < 900) return Column(children: spaced([...left, ...right]));
            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(child: Column(children: spaced(left))),
                const SizedBox(width: 18),
                Expanded(child: Column(children: spaced(right))),
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
            borderRadius: BorderRadius.circular(12),
            onTap: on ? null : () => s.updateSettings((x) => x['routing']['mode'] = mode),
            child: AnimatedContainer(
              duration: const Duration(milliseconds: 150),
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: on ? Color.alphaBlend(accent.withValues(alpha: .1), p.surface) : p.surface,
                borderRadius: BorderRadius.circular(12),
                border: Border.all(color: on ? accent : p.border, width: on ? 1.5 : 1),
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

  /// The ready-made sets: Russian sites direct (not in the "only selected"
  /// mode, where they go direct anyway).
  Widget _presetsPanel(BuildContext context, {required bool selected}) {
    final russia = !selected && s.hasSetting('routing.russia_direct');
    final russiaOn = s.setting('routing.russia_direct', false);
    void set(String key, bool v) => s.updateSettings((x) => x['routing'][key] = v);
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle(tr('rules.presets.title')),
          if (russia)
            SettingRow(
              first: true,
              title: tr('rules.presets.russia.title'),
              description: tr('rules.presets.russia.desc'),
              trailing: Switch(value: russiaOn, onChanged: (v) => set('russia_direct', v)),
            ),
          if (russia && russiaOn && s.hasSetting('routing.russia_abroad'))
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

  /// The user's own rules and where their categories come from; an older
  /// service knows neither.
  List<Widget> _ownRules() => [
    if (s.hasSetting('routing.rules')) UserRulesPanel(state: s),
    if (s.hasSetting('routing.geo'))
      Fold(
        title: tr('rules.geo.fold'),
        sub: geoFoldSub(s),
        child: GeoSourcePanel(state: s),
      ),
  ];

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
