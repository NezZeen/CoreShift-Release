import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../api/models.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../../state/import_link.dart';
import '../countries.dart';
import '../qr.dart';
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

/// A run of rows under one heading: a country, a
/// subscription, or the whole list with none.
class _Section {
  final String id;
  final String? title;
  final Widget? leading;
  final List<(Subscription, NodeView)> rows;

  /// Whether the heading folds the rows away.
  final bool collapsible;

  /// Whether each row names its subscription.
  final bool showSub;
  const _Section({required this.id, this.title, this.leading, required this.rows, this.collapsible = false, this.showSub = false});
}

class _ServersPageState extends State<ServersPage> {
  String? subFilter; // null = all subscriptions
  String query = '';

  /// Folded countries and subscriptions; kept, so they stay folded when
  /// the user comes back.
  late final collapsed = <String>{...?(widget.state.prefs['servers_folded'] as List?)?.whereType<String>()};

  AppState get s => widget.state;

  void _toggle(String id) {
    setState(() => collapsed.contains(id) ? collapsed.remove(id) : collapsed.add(id));
    s.setPref('servers_folded', collapsed.toList());
  }

  @override
  void initState() {
    super.initState();
    if (!s.latencyAutoTested && s.online && !s.testingLatency && s.latency.isEmpty && s.nodeCount > 0) {
      s.latencyAutoTested = true;
      // Not while the page is being built: the test notifies listeners.
      WidgetsBinding.instance.addPostFrameCallback((_) => s.testLatency());
    }
  }

  /// The rows in the groups the user chose, in the order the subscription
  /// lists them: by country, or by subscription, or as one list.
  List<_Section> _sections(List<(Subscription, NodeView)> rows, {required bool multi}) {
    final rest = rows;
    final out = <_Section>[];
    if (s.serverGroup) {
      final by = <String, List<(Subscription, NodeView)>>{};
      for (final r in rest) {
        (by[countryOf(r.$2.name, r.$2.server) ?? ''] ??= []).add(r);
      }
      final codes = by.keys.toList()
        ..sort((a, b) {
          if (a.isEmpty != b.isEmpty) return a.isEmpty ? 1 : -1;
          return countryName(a).compareTo(countryName(b));
        });
      for (final code in codes) {
        out.add(
          _Section(
            id: 'c:$code',
            title: code.isEmpty ? 'Другие' : countryName(code),
            leading: CountryBadge(code, width: 26),
            rows: by[code]!,
            collapsible: true,
            showSub: multi,
          ),
        );
      }
    } else if (multi) {
      for (final sub in s.subscriptions) {
        final mine = rest.where((r) => r.$1.id == sub.id).toList();
        if (mine.isNotEmpty) out.add(_Section(id: 's:${sub.id}', title: sub.displayName, rows: mine, collapsible: true));
      }
    } else if (rest.isNotEmpty) {
      out.add(_Section(id: 'all', rows: rest, showSub: multi));
    }
    return out;
  }

  @override
  Widget build(BuildContext context) {
    final subs = s.subscriptions;
    if (subFilter != null && s.subscriptionById(subFilter!) == null) subFilter = null;
    final rows = <(Subscription, NodeView)>[
      for (final sub in subs)
        if (subFilter == null || sub.id == subFilter)
          for (final n in sub.nodes)
            if (_matches(n, sub)) (sub, n),
    ];
    final compact = isCompact(context);
    final sections = _sections(rows, multi: subs.length > 1 && subFilter == null);

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
    // The search finds protocols too, so there is no protocol filter; the
    // fastest server is picked in the home page's quick pick.
    final chips = [
      _Chip(label: 'По странам', on: s.serverGroup, onTap: () => s.setPref('server_group', !s.serverGroup)),
      if (subFilter != null) _Chip(label: '× ${s.subscriptionById(subFilter!)?.displayName ?? ''}', on: true, onTap: () => setState(() => subFilter = null)),
    ];
    // How to ping is the service's choice: a ping, or a request through the
    // core for a server a ping cannot time.
    final ping = Btn(
      label: 'Проверить пинг',
      icon: Icons.speed,
      small: true,
      loading: s.testingLatency,
      tooltip: subFilter == null ? 'Проверить все серверы' : 'Проверить серверы этой подписки',
      onPressed: () => s.testLatency(subFilter),
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
        else
          LayoutBuilder(
            builder: (context, c) {
              Widget card(Subscription sub) =>
                  _SubCard(state: s, sub: sub, selected: subFilter == sub.id, onTap: () => setState(() => subFilter = subFilter == sub.id ? null : sub.id));
              final list = _serverList(context, compact: compact, search: search, chips: chips, ping: ping, rows: rows, sections: sections);
              // A wide window: the subscriptions as a column beside the
              // servers, like folders beside their files.
              if (!compact && c.maxWidth >= 940) {
                return Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    SizedBox(
                      width: 330,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          for (final (i, sub) in subs.indexed) ...[if (i > 0) const SizedBox(height: 12), card(sub)],
                          if (subs.length > 1) ...[
                            const SizedBox(height: 10),
                            Text(
                              subFilter == null ? 'Нажмите на подписку, чтобы показать только её серверы' : 'Нажмите ещё раз, чтобы показать все',
                              style: TextStyle(fontSize: 11.5, color: context.pal.dim),
                            ),
                          ],
                        ],
                      ),
                    ),
                    const SizedBox(width: 20),
                    Expanded(child: list),
                  ],
                );
              }
              // A phone: the subscriptions side by side, swiped through,
              // so the servers start high on the screen.
              if (compact) {
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    if (subs.length == 1)
                      card(subs.single)
                    else
                      SingleChildScrollView(
                        scrollDirection: Axis.horizontal,
                        clipBehavior: Clip.none,
                        child: Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            for (final (i, sub) in subs.indexed) ...[
                              if (i > 0) const SizedBox(width: 10),
                              SizedBox(width: min(300, c.maxWidth - 44), child: card(sub)),
                            ],
                          ],
                        ),
                      ),
                    const SizedBox(height: 14),
                    list,
                  ],
                );
              }
              final cols = (c.maxWidth / 272).floor().clamp(1, 4);
              final w = (c.maxWidth - 12 * (cols - 1)) / cols;
              return Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Wrap(
                    spacing: 12,
                    runSpacing: 12,
                    children: [for (final sub in subs) SizedBox(width: w, child: card(sub))],
                  ),
                  const SizedBox(height: 18),
                  list,
                ],
              );
            },
          ),
      ],
    );
  }

  /// The search, the filters and the servers.
  Widget _serverList(
    BuildContext context, {
    required bool compact,
    required Widget search,
    required List<Widget> chips,
    required Btn ping,
    required List<(Subscription, NodeView)> rows,
    required List<_Section> sections,
  }) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (compact) ...[
          // A phone: the search with the ping as an icon, then the filters.
          Row(
            children: [
              Expanded(child: search),
              const SizedBox(width: 8),
              Btn(icon: ping.icon, tooltip: ping.tooltip, loading: ping.loading, onPressed: ping.onPressed),
            ],
          ),
          const SizedBox(height: 10),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: [
                for (final (i, c) in chips.indexed) ...[if (i > 0) const SizedBox(width: 6), c],
              ],
            ),
          ),
        ] else
          Row(
            children: [
              Expanded(child: search),
              const SizedBox(width: 10),
              for (final c in chips) ...[c, const SizedBox(width: 10)],
              ping,
            ],
          ),
        if (compact && rows.isNotEmpty && s.prefs['swipe_hint'] != true) _SwipeHint(onClose: () => s.setPref('swipe_hint', true)),
        const SizedBox(height: 12),
        Panel(
          padding: const EdgeInsets.all(6),
          child: _NodeTable(
            state: s,
            sections: sections,
            // A search shows what it found, folded or not.
            collapsed: query.isEmpty ? collapsed : const {},
            onToggle: _toggle,
          ),
        ),
      ],
    );
  }

  bool _matches(NodeView n, Subscription sub) {
    if (query.isEmpty) return true;
    final code = countryOf(n.name, n.server);
    return n.name.toLowerCase().contains(query) ||
        n.server.toLowerCase().contains(query) ||
        protocolLabel(n.protocol).toLowerCase().contains(query) ||
        n.protocol.toLowerCase().contains(query) ||
        (query == 'hy2' && n.protocol == 'hysteria2') ||
        (query == 'ss' && n.protocol == 'shadowsocks') ||
        (code != null && (code.toLowerCase() == query || countryName(code).toLowerCase().contains(query))) ||
        sub.displayName.toLowerCase().contains(query);
  }
}

/// "5 серверов", with the Russian plural.
String serversCount(int n) => '$n ${ruPlural(n, 'сервер', 'сервера', 'серверов')}';
