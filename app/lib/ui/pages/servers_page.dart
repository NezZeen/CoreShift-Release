import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../api/models.dart';
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../support.dart';
import '../theme.dart';
import '../widgets.dart';

part 'servers/cards.dart';
part 'servers/nodes.dart';
part 'servers/dialogs.dart';

class ServersPage extends StatefulWidget {
  final AppState state;

  /// Focused by Ctrl+F.
  final FocusNode? searchFocus;
  const ServersPage({super.key, required this.state, this.searchFocus});

  @override
  State<ServersPage> createState() => _ServersPageState();
}

class _ServersPageState extends State<ServersPage> {
  String? subFilter; // null = all subscriptions
  String protoFilter = '';
  String query = '';

  AppState get s => widget.state;

  @override
  void initState() {
    super.initState();
    if (!s.latencyAutoTested && s.online && !s.testingLatency && s.latency.isEmpty && s.nodeCount > 0) {
      s.latencyAutoTested = true;
      // Not while the page is being built: the test notifies listeners.
      WidgetsBinding.instance.addPostFrameCallback((_) => s.testLatency());
    }
  }

  /// The fastest working node among [rows], if any was tested.
  (Subscription, NodeView)? _fastest(List<(Subscription, NodeView)> rows) {
    (Subscription, NodeView)? best;
    var bestMs = 0;
    for (final (sub, n) in rows) {
      final l = s.latencyOf(sub.id, n.fingerprint);
      if (l != null && l.ok && n.cores.isNotEmpty && (best == null || l.ms < bestMs)) {
        best = (sub, n);
        bestMs = l.ms;
      }
    }
    return best;
  }

  @override
  Widget build(BuildContext context) {
    final subs = s.subscriptions;
    if (subFilter != null && s.subscriptionById(subFilter!) == null) subFilter = null;
    final rows = <(Subscription, NodeView)>[
      for (final sub in subs)
        if (subFilter == null || sub.id == subFilter)
          for (final n in sub.nodes)
            if ((protoFilter.isEmpty || n.protocol == protoFilter) && _matches(n, sub)) (sub, n),
    ];
    final protocols = {
      for (final sub in subs)
        for (final n in sub.nodes) n.protocol,
    }.toList()..sort();
    final fastest = _fastest(rows);
    final compact = isCompact(context);

    final search = TextField(
      focusNode: widget.searchFocus,
      onChanged: (v) => setState(() => query = v.trim().toLowerCase()),
      style: const TextStyle(fontSize: 13),
      decoration: InputDecoration(
        hintText: compact ? 'Поиск' : 'Поиск по названию, стране, протоколу (Ctrl+F)',
        prefixIcon: Icon(Icons.search, size: 17, color: context.pal.dim),
        prefixIconConstraints: const BoxConstraints(minWidth: 34),
      ),
    );
    // The protocols in a menu rather than a row of chips that wraps.
    final protocol = PopupMenuButton<String>(
      tooltip: 'Показать только один протокол',
      onSelected: (v) => setState(() => protoFilter = v),
      itemBuilder: (_) => [
        CheckedPopupMenuItem(value: '', checked: protoFilter.isEmpty, child: const Text('Все протоколы')),
        for (final pr in protocols) CheckedPopupMenuItem(value: pr, checked: protoFilter == pr, child: Text(protocolLabel(pr))),
      ],
      child: IgnorePointer(
        child: Btn(label: protoFilter.isEmpty ? 'Все протоколы' : protocolLabel(protoFilter), icon: Icons.filter_list, small: true, onPressed: () {}),
      ),
    );
    final chips = [
      protocol,
      if (subFilter != null) _Chip(label: '× ${s.subscriptionById(subFilter!)?.displayName ?? ''}', on: true, onTap: () => setState(() => subFilter = null)),
    ];
    final pingMode = s.setting('cores.latency_test', 'ping');
    // How to ping, next to the button that pings.
    final pingHow = s.hasSetting('cores.latency_test')
        ? PopupMenuButton<String>(
            tooltip: 'Как проверять пинг',
            onSelected: (v) => s.updateSettings((x) => x['cores']['latency_test'] = v),
            itemBuilder: (_) => [
              for (final (v, title, text) in const [
                ('ping', 'Пинг до сервера', 'Время TCP-подключения к серверу: быстро, без запуска ядер'),
                ('proxy', 'Запрос через ядро', 'Реальная задержка с шифрованием, заодно видно, работает ли сервер'),
              ])
                CheckedPopupMenuItem(
                  value: v,
                  checked: pingMode == v,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Text(title),
                      Text(text, style: TextStyle(fontSize: 11, color: context.pal.muted)),
                    ],
                  ),
                ),
            ],
            child: IgnorePointer(
              child: Btn(icon: Icons.tune, small: true, onPressed: () {}),
            ),
          )
        : null;
    final ping = Btn(
      label: 'Проверить пинг',
      icon: Icons.speed,
      small: true,
      loading: s.testingLatency,
      tooltip: subFilter == null ? 'Проверить все серверы' : 'Проверить серверы этой подписки',
      onPressed: () => s.testLatency(subFilter),
    );
    final best = Btn(
      label: 'Самый быстрый',
      icon: Icons.bolt,
      small: true,
      tooltip: fastest == null ? 'Сначала запустите тест задержки' : '${fastest.$2.name}: ${s.latencyOf(fastest.$1.id, fastest.$2.fingerprint)!.ms} мс',
      onPressed: fastest == null ? null : () => s.selectNode(fastest.$1.id, fastest.$2.fingerprint, fastest.$2.name),
    );

    return PageFrame(
      children: [
        PageHeader(
          'Серверы',
          subtitle: 'Двойной щелчок по серверу подключает к нему.',
          actions: [
            if (subs.any((x) => !x.isLocal))
              Btn(
                label: compact ? null : 'Обновить все',
                icon: Icons.refresh,
                tooltip: compact ? 'Обновить подписки' : null,
                loading: s.refreshing.isNotEmpty,
                onPressed: s.refreshAll,
              ),
            Btn(label: compact ? 'Добавить' : 'Добавить подписку', icon: Icons.add, kind: BtnKind.primary, onPressed: () => showAddSubscription(context, s)),
          ],
        ),
        if (subs.isEmpty)
          _EmptySubs(onAdd: () => showAddSubscription(context, s))
        else ...[
          LayoutBuilder(
            builder: (context, c) {
              final cols = (c.maxWidth / 272).floor().clamp(1, 4);
              final w = (c.maxWidth - 12 * (cols - 1)) / cols;
              return Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  for (final sub in subs)
                    SizedBox(
                      width: w,
                      child: _SubCard(
                        state: s,
                        sub: sub,
                        selected: subFilter == sub.id,
                        onTap: () => setState(() => subFilter = subFilter == sub.id ? null : sub.id),
                      ),
                    ),
                ],
              );
            },
          ),
          SizedBox(height: compact ? 14 : 18),
          if (compact) ...[
            // A phone: the search with the buttons as icons, then one
            // sideways row of filters.
            Row(
              children: [
                Expanded(child: search),
                const SizedBox(width: 8),
                Btn(icon: ping.icon, tooltip: ping.tooltip, loading: ping.loading, onPressed: ping.onPressed),
                const SizedBox(width: 6),
                Btn(icon: best.icon, tooltip: best.tooltip, onPressed: best.onPressed),
              ],
            ),
            const SizedBox(height: 10),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children: [
                  for (final (i, c) in chips.indexed) ...[if (i > 0) const SizedBox(width: 6), c],
                  if (pingHow != null) ...[const SizedBox(width: 6), pingHow],
                ],
              ),
            ),
          ] else
            Wrap(
              spacing: 12,
              runSpacing: 10,
              crossAxisAlignment: WrapCrossAlignment.center,
              children: [
                SizedBox(width: 320, child: search),
                ...chips,
                Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    ping,
                    if (pingHow != null) ...[const SizedBox(width: 4), pingHow],
                  ],
                ),
                best,
              ],
            ),
          const SizedBox(height: 12),
          Panel(
            padding: const EdgeInsets.all(6),
            child: _NodeTable(state: s, rows: rows, showSub: subs.length > 1 && subFilter == null),
          ),
        ],
      ],
    );
  }

  bool _matches(NodeView n, Subscription sub) {
    if (query.isEmpty) return true;
    return n.name.toLowerCase().contains(query) ||
        n.server.toLowerCase().contains(query) ||
        protocolLabel(n.protocol).toLowerCase().contains(query) ||
        n.transport.contains(query) ||
        sub.displayName.toLowerCase().contains(query);
  }
}

/// "5 серверов", with the Russian plural.
String serversCount(int n) {
  final m10 = n % 10, m100 = n % 100;
  final word = m10 == 1 && m100 != 11 ? 'сервер' : (m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) ? 'сервера' : 'серверов');
  return '$n $word';
}
