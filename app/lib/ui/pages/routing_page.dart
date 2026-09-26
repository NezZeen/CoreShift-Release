import 'package:flutter/material.dart';

import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../theme.dart';
import '../widgets.dart';
import 'direct_apps.dart';
import 'rule_lists.dart';

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
          title: 'Сайты и адреса через VPN',
          sub: 'сайт и все его поддомены',
          description: 'Только они идут через VPN. Всё остальное открывается напрямую, как без VPN.',
          domainsKey: 'routing.proxy_domains',
          ipsKey: 'routing.proxy_ips',
          hint: 'youtube.com, 91.108.4.0/22',
          empty: 'Пока пусто — всё идёт напрямую',
          removeTip: 'Больше не через VPN',
          chipColor: accent,
        ),
      ];
      right = [
        if (!platform.isAndroid)
          AppsPanel.proxy(
            state: s,
            description: 'Весь трафик этих программ идёт через VPN, куда бы они ни подключались. Удобно для мессенджеров и игр, заблокированных целиком.',
          ),
        _blockPanel(),
        _localPanel(context),
      ];
    } else {
      left = [
        _presetsPanel(context),
        RuleListPanel(
          key: const ValueKey('all-direct'),
          state: s,
          title: 'Сайты и адреса без VPN',
          sub: 'сайт и все его поддомены',
          description: full ? 'Можно указать IP-адрес или подсеть, например 10.8.0.0/16 для рабочей сети.' : null,
          domainsKey: 'routing.direct_domains',
          ipsKey: 'routing.direct_ips',
          hint: full ? 'gosuslugi.ru, 10.8.0.0/16' : 'gosuslugi.ru, bank.example',
          empty: 'Пока пусто — всё идёт через VPN',
          removeTip: 'Снова через VPN',
        ),
        if (full)
          RuleListPanel(
            key: const ValueKey('all-proxy'),
            state: s,
            title: 'Всегда через VPN',
            sub: 'важнее исключений',
            description: 'Для сайтов, которые попали под исключения, но без VPN не открываются: например, заблокированный сайт в зоне .ru.',
            domainsKey: 'routing.proxy_domains',
            ipsKey: 'routing.proxy_ips',
            hint: 'blocked.ru',
            empty: 'Пока пусто',
            removeTip: 'Убрать из списка',
            chipColor: accent,
          ),
      ];
      // Programs are matched by their .exe; Android has no such thing.
      right = [if (s.hasSetting('routing.direct_apps') && !platform.isAndroid) AppsPanel.direct(state: s), if (full) _blockPanel()];
    }

    return PageFrame(
      children: [
        PageHeader(
          'Правила',
          subtitle: selected
              ? 'Через VPN идут только выбранные сайты и программы, остальное — напрямую.'
              : 'Сайты и программы, которые работают напрямую, без VPN. Всё остальное идёт через VPN.',
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

  Widget _notice(BuildContext context, bool tun) {
    final p = context.pal;
    // On a phone only the warning: the rest the home page says itself.
    if (tun && isCompact(context)) return const SizedBox();
    final color = tun ? swapColor : warnColor;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      margin: const EdgeInsets.only(bottom: 18),
      decoration: BoxDecoration(
        color: color.withValues(alpha: .07),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: color.withValues(alpha: .3)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(tun ? Icons.swap_horiz : Icons.info_outline, size: 17, color: color),
          const SizedBox(width: 10),
          Expanded(
            child: Text.rich(
              TextSpan(
                style: TextStyle(fontSize: 13, color: p.muted),
                children: tun
                    ? [
                        TextSpan(
                          text: 'Работает в режиме «Все приложения». ',
                          style: TextStyle(color: p.text, fontWeight: FontWeight.w600),
                        ),
                        const TextSpan(
                          text:
                              'Правила не зависят от ядра и не теряются при его смене. '
                              'Изменения применяются при следующем подключении.',
                        ),
                      ]
                    : [
                        TextSpan(
                          text: 'Сейчас включён режим «Только прокси». ',
                          style: TextStyle(color: p.text, fontWeight: FontWeight.w600),
                        ),
                        const TextSpan(
                          text:
                              'В нём через VPN идут только программы, настроенные на прокси, и списки ниже не действуют. '
                              'Они заработают в режиме «Все приложения».',
                        ),
                      ],
              ),
            ),
          ),
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
              padding: const EdgeInsets.all(14),
              decoration: BoxDecoration(
                color: on ? accent.withValues(alpha: .1) : p.surface,
                borderRadius: BorderRadius.circular(12),
                border: Border.all(color: on ? accent.withValues(alpha: .7) : p.border, width: on ? 1.5 : 1),
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(on ? Icons.radio_button_checked : Icons.radio_button_off, size: 18, color: on ? accent : p.dim),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Icon(icon, size: 15, color: on ? p.text : p.muted),
                            const SizedBox(width: 6),
                            Flexible(
                              child: Text(title, style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
                            ),
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

    const allText = 'Кроме исключений. Скрывает весь трафик, российские сайты можно пустить напрямую.';
    const onlyText = 'Через VPN идут только выбранные сервисы, остальное напрямую. Банки и российские сайты работают как без VPN.';
    if (isCompact(context)) {
      // A phone: a switch of two words and what the chosen mode does.
      return Builder(
        builder: (context) => Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Seg<bool>(
              value: selected,
              options: const [(false, 'Всё через VPN'), (true, 'Только выбранное')],
              onChanged: (v) => s.updateSettings((x) => x['routing']['mode'] = v ? 'selected' : 'all'),
            ),
            const SizedBox(height: 8),
            Text(selected ? onlyText : allText, style: TextStyle(fontSize: 12, color: context.pal.muted, height: 1.4)),
          ],
        ),
      );
    }
    final all = option('all', Icons.shield_outlined, 'Всё через VPN', allText);
    final only = option('selected', Icons.filter_alt_outlined, 'Только выбранное', onlyText);
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

  Widget _presetsPanel(BuildContext context) {
    final p = context.pal;
    // What always goes direct is news only to the curious: not on a phone.
    final always = !isCompact(context);
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Готовые наборы'),
          if (s.hasSetting('routing.russia_direct'))
            SettingRow(
              first: true,
              title: 'Российские сайты напрямую',
              description:
                  'Госуслуги, банки, Яндекс, VK и другие российские сервисы откроются без VPN — '
                  'быстрее и без блокировок «из-за границы». Заблокированные в России СМИ остаются в VPN, даже на .ru. '
                  'Списки обновляются сами раз в неделю.',
              trailing: Switch(value: s.setting('routing.russia_direct', false), onChanged: (v) => s.updateSettings((x) => x['routing']['russia_direct'] = v)),
            ),
          if (always)
            SettingRow(
              title: 'Локальная сеть напрямую',
              descriptionWidget: Text(
                '10.0.0.0/8, 192.168.0.0/16, .lan, .local, .home.arpa',
                style: TextStyle(fontSize: 12, color: p.muted, fontFamily: monoFont),
              ),
              trailing: Text('всегда', style: TextStyle(fontSize: 12, color: p.dim)),
            ),
          if (always)
            SettingRow(
              title: 'Адрес VPN-сервера напрямую',
              description: 'Чтобы туннель не шёл сам через себя',
              trailing: Text('всегда', style: TextStyle(fontSize: 12, color: p.dim)),
            ),
        ],
      ),
    );
  }

  /// In the "only selected" mode everything else is direct anyway; this
  /// just says the local network stays reachable.
  Widget _localPanel(BuildContext context) {
    final p = context.pal;
    return Panel(
      child: Row(
        children: [
          Icon(Icons.lan_outlined, size: 17, color: p.muted),
          const SizedBox(width: 10),
          Expanded(
            child: Text('Локальная сеть и российские сайты в этом режиме и так работают напрямую.', style: TextStyle(fontSize: 12, color: p.muted)),
          ),
        ],
      ),
    );
  }

  Widget _blockPanel() => RuleListPanel(
    key: const ValueKey('block'),
    state: s,
    title: 'Блокировать',
    sub: 'не открывать совсем',
    description: 'Сайты из этого списка не откроются ни через VPN, ни напрямую: реклама, трекеры, отвлекающие сайты.',
    domainsKey: 'routing.block_domains',
    hint: 'ads.example',
    empty: 'Пока пусто',
    removeTip: 'Разблокировать',
    chipColor: errColor,
  );
}
