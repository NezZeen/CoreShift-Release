part of '../servers_page.dart';

/// The preference that hides the phone's hint about the swipes and the
/// long press, once the user has used one or closed it.
const serverHintPref = 'server_hint';

/// What can be done with one server: the desktop's «⋯» menu and right
/// click, the phone's long press.
enum _NodeAction { connect, favorite, hide }

/// The servers in the order [sort] names: "ping" puts the fastest first,
/// then those not tested yet and those that did not answer; "name" goes by
/// the name without its flag, numbers as numbers («Server 2» before
/// «Server 10»); "sub" keeps the subscription's order, as ties do.
List<(Subscription, NodeView)> sortServers(List<(Subscription, NodeView)> rows, String sort, Latency? Function(Subscription, NodeView) latencyOf) {
  if (sort != 'ping' && sort != 'name') return rows;
  int ping((Subscription, NodeView) r) {
    final l = latencyOf(r.$1, r.$2);
    if (l == null) return 1 << 30;
    return l.ok && r.$2.cores.isNotEmpty ? l.ms : (1 << 30) + 1;
  }

  final keyed = [for (final (i, r) in rows.indexed) (i, r, sort == 'ping' ? ping(r) : 0, sort == 'name' ? cleanNodeName(r.$2.name).toLowerCase() : '')];
  keyed.sort((a, b) {
    final c = sort == 'ping' ? a.$3.compareTo(b.$3) : compareNatural(a.$4, b.$4);
    return c != 0 ? c : a.$1.compareTo(b.$1);
  });
  return [for (final k in keyed) k.$2];
}

/// Compares [a] and [b] with the runs of digits in them as numbers.
int compareNatural(String a, String b) {
  final parts = RegExp(r'\d+|\D+');
  final x = [for (final m in parts.allMatches(a)) m[0]!], y = [for (final m in parts.allMatches(b)) m[0]!];
  for (var i = 0; i < x.length && i < y.length; i++) {
    final p = int.tryParse(x[i]), q = int.tryParse(y[i]);
    final c = p != null && q != null ? p.compareTo(q) : x[i].compareTo(y[i]);
    if (c != 0) return c;
  }
  return x.length.compareTo(y.length);
}

/// Whether [n] is the server the connection runs on, or is starting on:
/// it is neither connected again nor removed from the list.
bool _inUse(AppState s, Subscription sub, NodeView n) => s.isSelected(sub, n) && s.status.active;

bool _canConnect(AppState s, Subscription sub, NodeView n) => n.cores.isNotEmpty && !s.busy && s.online && !_inUse(s, sub, n);

List<PopupMenuEntry<_NodeAction>> _serverMenuItems(AppState s, Subscription sub, NodeView n) {
  final fav = s.isFavorite(sub, n);
  final inUse = _inUse(s, sub, n);
  PopupMenuItem<_NodeAction> item(_NodeAction v, IconData icon, String label, {Color? color, bool enabled = true, String? note}) => PopupMenuItem(
    value: v,
    height: note == null ? 38 : 48,
    enabled: enabled,
    child: Builder(
      builder: (context) {
        final p = context.pal;
        final c = !enabled ? p.dim : (color == null ? null : p.ink(color));
        return Row(
          children: [
            Icon(icon, size: 17, color: c ?? p.muted),
            const SizedBox(width: 12),
            Flexible(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(label, style: TextStyle(fontSize: 13.5, color: c ?? p.text)),
                  if (note != null) Text(note, style: TextStyle(fontSize: 11.5, color: p.dim)),
                ],
              ),
            ),
          ],
        );
      },
    ),
  );
  return [
    item(
      _NodeAction.connect,
      Icons.power_settings_new,
      inUse ? 'Подключён' : 'Подключиться',
      enabled: _canConnect(s, sub, n),
      note: n.cores.isEmpty ? 'Ни одно ядро его не поддерживает' : null,
    ),
    item(
      _NodeAction.favorite,
      fav ? Icons.star_rounded : Icons.star_outline_rounded,
      fav ? 'Убрать из избранного' : 'В избранное',
      color: fav ? warnColor : null,
    ),
    const PopupMenuDivider(height: 9),
    item(
      _NodeAction.hide,
      Icons.delete_outline,
      'Удалить из подписки',
      color: errColor,
      enabled: !inUse,
      note: inUse ? 'Сначала подключитесь к другому' : null,
    ),
  ];
}

void _runServerAction(AppState s, Subscription sub, NodeView n, _NodeAction a) {
  switch (a) {
    case _NodeAction.connect:
      if (_canConnect(s, sub, n)) s.connect(subscription: sub.id, fingerprint: n.fingerprint, name: n.name);
    case _NodeAction.favorite:
      s.toggleFavorite(sub, n);
    case _NodeAction.hide:
      if (!_inUse(s, sub, n)) s.hideNode(sub, n, label: cleanNodeName(n.name));
  }
}

/// A phone's long press on a server: what the desktop's «⋯» menu offers, as
/// a sheet with the server on top.
Future<void> showServerActions(BuildContext context, AppState s, Subscription sub, NodeView n) {
  final p = context.pal;
  return showModalBottomSheet<void>(
    context: context,
    backgroundColor: p.surface,
    showDragHandle: true,
    builder: (c) => SafeArea(
      child: ListenableBuilder(
        listenable: s,
        builder: (context, _) {
          final p = context.pal;
          final fav = s.isFavorite(sub, n);
          final inUse = _inUse(s, sub, n);
          final l = s.latencyOf(sub.id, n.fingerprint);
          void run(_NodeAction a) {
            Navigator.pop(context);
            _runServerAction(s, sub, n, a);
          }

          Widget row(_NodeAction a, IconData icon, String label, {Color? color, bool enabled = true, String? note}) {
            final c = !enabled ? p.dim : (color == null ? p.text : p.ink(color));
            return InkWell(
              onTap: enabled ? () => run(a) : null,
              borderRadius: BorderRadius.circular(10),
              child: Container(
                constraints: const BoxConstraints(minHeight: 52),
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                child: Row(
                  children: [
                    Icon(icon, size: 21, color: color == null && enabled ? p.muted : c),
                    const SizedBox(width: 16),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          Text(label, style: TextStyle(fontSize: 15, color: c)),
                          if (note != null) ...[const SizedBox(height: 2), Text(note, style: TextStyle(fontSize: 12, color: p.dim))],
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            );
          }

          return Padding(
            padding: const EdgeInsets.fromLTRB(12, 0, 12, 10),
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Padding(
                  padding: const EdgeInsets.fromLTRB(8, 0, 8, 12),
                  child: Row(
                    children: [
                      CountryBadge(countryOf(n.name, n.server), width: 34),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(cleanNodeName(n.name), style: display(16), overflow: TextOverflow.ellipsis),
                            const SizedBox(height: 4),
                            Row(
                              children: [
                                ProtoBadge(n.protocol),
                                const SizedBox(width: 8),
                                Flexible(
                                  child: Text(
                                    sub.displayName,
                                    style: TextStyle(fontSize: 12, color: p.dim),
                                    overflow: TextOverflow.ellipsis,
                                  ),
                                ),
                              ],
                            ),
                            const SizedBox(height: 3),
                            Text(
                              '${n.server}:${n.port}',
                              style: TextStyle(fontSize: 11.5, color: p.dim, fontFamily: monoFont, fontFamilyFallback: monoFallback),
                              overflow: TextOverflow.ellipsis,
                            ),
                          ],
                        ),
                      ),
                      if (l != null) ...[const SizedBox(width: 8), _LatencyCell(latency: l, testing: false)],
                    ],
                  ),
                ),
                Divider(height: 1, color: p.border),
                const SizedBox(height: 6),
                row(
                  _NodeAction.connect,
                  Icons.power_settings_new,
                  inUse ? 'Подключён' : 'Подключиться',
                  enabled: _canConnect(s, sub, n),
                  note: n.cores.isEmpty ? 'Ни одно ядро его не поддерживает' : null,
                ),
                row(
                  _NodeAction.favorite,
                  fav ? Icons.star_rounded : Icons.star_outline_rounded,
                  fav ? 'Убрать из избранного' : 'В избранное',
                  color: fav ? warnColor : null,
                ),
                row(
                  _NodeAction.hide,
                  Icons.delete_outline,
                  'Удалить из подписки',
                  color: errColor,
                  enabled: !inUse,
                  note: inUse ? 'Сначала подключитесь к другому серверу' : 'Не вернётся при обновлении подписки',
                ),
              ],
            ),
          );
        },
      ),
    ),
  );
}

/// Under the list: how many servers were removed from the subscriptions,
/// and the way to bring them back.
class _HiddenFooter extends StatelessWidget {
  final int count;
  final VoidCallback onShow;
  const _HiddenFooter({required this.count, required this.onShow});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: const EdgeInsets.fromLTRB(10, 10, 4, 0),
      child: Wrap(
        crossAxisAlignment: WrapCrossAlignment.center,
        spacing: 6,
        children: [
          Icon(Icons.visibility_off_outlined, size: 15, color: p.dim),
          Text('Удалено из подписок: $count', style: TextStyle(fontSize: 12.5, color: p.muted)),
          TextButton(
            onPressed: onShow,
            style: TextButton.styleFrom(
              foregroundColor: p.accentInk,
              padding: const EdgeInsets.symmetric(horizontal: 6),
              minimumSize: const Size(0, 30),
              tapTargetSize: MaterialTapTargetSize.shrinkWrap,
              textStyle: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600),
            ),
            child: const Text('Показать и вернуть'),
          ),
        ],
      ),
    );
  }
}

/// The servers removed from the subscriptions in [scope] (all when null),
/// each with a way back, and all at once.
Future<void> showHiddenServers(BuildContext context, AppState s, {String? scope}) => showDialog<void>(
  context: context,
  builder: (context) => _HiddenDialog(state: s, scope: scope),
);

class _HiddenDialog extends StatefulWidget {
  final AppState state;
  final String? scope;
  const _HiddenDialog({required this.state, this.scope});

  @override
  State<_HiddenDialog> createState() => _HiddenDialogState();
}

class _HiddenDialogState extends State<_HiddenDialog> {
  /// The servers being brought back, by subscription and fingerprint.
  final busy = <String>{};

  AppState get s => widget.state;

  List<(Subscription, NodeView)> get _hidden => [
    for (final sub in s.subscriptions)
      if (widget.scope == null || sub.id == widget.scope)
        for (final n in sub.hiddenNodes) (sub, n),
  ];

  Future<void> _restore(List<(Subscription, NodeView)> rows) async {
    setState(() => busy.addAll([for (final (sub, n) in rows) AppStateServers.key(sub.id, n.fingerprint)]));
    final bySub = <String, List<String>>{};
    for (final (sub, n) in rows) {
      (bySub[sub.id] ??= []).add(n.fingerprint);
    }
    for (final e in bySub.entries) {
      await s.setNodesHidden(e.key, e.value, hidden: false);
    }
    if (mounted) setState(() => busy.removeAll([for (final (sub, n) in rows) AppStateServers.key(sub.id, n.fingerprint)]));
  }

  @override
  Widget build(BuildContext context) {
    final compact = isCompact(context);
    return Dialog(
      insetPadding: compact ? const EdgeInsets.symmetric(horizontal: 14, vertical: 24) : null,
      child: SizedBox(
        width: 480,
        child: ListenableBuilder(
          listenable: s,
          builder: (context, _) {
            final p = context.pal;
            final rows = _hidden;
            final multi = rows.map((r) => r.$1.id).toSet().length > 1;
            return Padding(
              padding: EdgeInsets.all(compact ? 18 : 22),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text('Удалённые серверы', style: dialogTitle),
                  const SizedBox(height: 4),
                  Text(
                    'Их нет в списке, в проверке пинга и среди серверов, на которые CoreShift переключается сам. '
                    'Обновление подписки их не вернёт.',
                    style: TextStyle(fontSize: 12, color: p.muted, height: 1.35),
                  ),
                  const SizedBox(height: 14),
                  if (rows.isEmpty)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 24),
                      child: Text(
                        'Все серверы снова в списке',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: p.dim),
                      ),
                    )
                  else
                    Flexible(
                      child: Container(
                        decoration: BoxDecoration(
                          border: Border.all(color: p.border),
                          borderRadius: BorderRadius.circular(10),
                        ),
                        child: ListView.separated(
                          shrinkWrap: true,
                          padding: const EdgeInsets.symmetric(vertical: 4),
                          itemCount: rows.length,
                          separatorBuilder: (_, _) => Divider(height: 1, indent: 12, endIndent: 12, color: p.border),
                          itemBuilder: (context, i) {
                            final (sub, n) = rows[i];
                            final k = AppStateServers.key(sub.id, n.fingerprint);
                            return Padding(
                              padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
                              child: Row(
                                children: [
                                  CountryBadge(countryOf(n.name, n.server), width: 28),
                                  const SizedBox(width: 12),
                                  Expanded(
                                    child: Column(
                                      crossAxisAlignment: CrossAxisAlignment.start,
                                      children: [
                                        Text(
                                          cleanNodeName(n.name),
                                          style: const TextStyle(fontWeight: FontWeight.w500),
                                          overflow: TextOverflow.ellipsis,
                                        ),
                                        const SizedBox(height: 3),
                                        Row(
                                          children: [
                                            ProtoBadge(n.protocol),
                                            if (multi) ...[
                                              const SizedBox(width: 8),
                                              Flexible(
                                                child: Text(
                                                  sub.displayName,
                                                  style: TextStyle(fontSize: 11.5, color: p.dim),
                                                  overflow: TextOverflow.ellipsis,
                                                ),
                                              ),
                                            ],
                                          ],
                                        ),
                                      ],
                                    ),
                                  ),
                                  const SizedBox(width: 8),
                                  Btn(
                                    label: compact ? null : 'Вернуть',
                                    tooltip: compact ? 'Вернуть в список' : null,
                                    icon: Icons.undo,
                                    small: true,
                                    loading: busy.contains(k),
                                    onPressed: () => _restore([(sub, n)]),
                                  ),
                                ],
                              ),
                            );
                          },
                        ),
                      ),
                    ),
                  const SizedBox(height: 18),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      Btn(label: 'Закрыть', onPressed: () => Navigator.pop(context)),
                      if (rows.length > 1) ...[
                        const SizedBox(width: 8),
                        Btn(label: 'Вернуть все', icon: Icons.undo, kind: BtnKind.primary, loading: busy.isNotEmpty, onPressed: () => _restore(rows)),
                      ],
                    ],
                  ),
                ],
              ),
            );
          },
        ),
      ),
    );
  }
}
