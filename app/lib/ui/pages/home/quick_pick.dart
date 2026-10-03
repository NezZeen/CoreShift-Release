part of '../home_page.dart';

/// Opens the quick server pick: a sheet on a phone, a window on the desktop.
/// [onAll] goes to the full list of servers.
Future<void> showQuickPick(BuildContext context, AppState state, {required VoidCallback onAll}) {
  final p = context.pal;
  if (isCompact(context)) {
    return showModalBottomSheet<void>(
      context: context,
      backgroundColor: p.surface,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (c) => SafeArea(
        child: ConstrainedBox(
          constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(c).height * .82),
          child: _QuickPick(state: state, onAll: onAll),
        ),
      ),
    );
  }
  return showDialog<void>(
    context: context,
    builder: (c) => Dialog(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 460),
        child: _QuickPick(state: state, onAll: onAll),
      ),
    ),
  );
}

/// The servers used last, the fastest one on top, and a
/// way to the whole list: what most people want when they change server.
class _QuickPick extends StatelessWidget {
  final AppState state;
  final VoidCallback onAll;
  const _QuickPick({required this.state, required this.onAll});

  @override
  Widget build(BuildContext context) {
    final compact = isCompact(context);
    return ListenableBuilder(
      listenable: state,
      builder: (context, _) {
        final p = context.pal;
        final fastest = state.fastestNode();
        final nodes = state.quickNodes();
        void pick(Subscription sub, NodeView n) {
          Navigator.pop(context);
          state.selectNode(sub.id, n.fingerprint, n.name);
        }

        return SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(compact ? 12 : 18, compact ? 0 : 18, compact ? 12 : 18, compact ? 12 : 16),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(4, 0, 4, 12),
                child: Row(
                  children: [
                    Expanded(child: Text('Выбор сервера', style: dialogTitle)),
                    if (!compact)
                      IconButton(
                        onPressed: () => Navigator.pop(context),
                        icon: Icon(Icons.close, size: 18, color: p.muted),
                        tooltip: 'Закрыть',
                      ),
                  ],
                ),
              ),
              _FastestTile(state: state, fastest: fastest, onPick: pick),
              const SizedBox(height: 14),
              const Padding(padding: EdgeInsets.only(left: 4), child: SectionLabel('Недавние')),
              if (nodes.isEmpty)
                Padding(
                  padding: const EdgeInsets.fromLTRB(4, 4, 4, 8),
                  child: Text('Здесь появятся серверы, к которым вы подключались.', style: TextStyle(color: p.muted, fontSize: 13, height: 1.4)),
                )
              else
                for (final (sub, n) in nodes) _QuickRow(state: state, sub: sub, node: n, onTap: () => pick(sub, n)),
              const SizedBox(height: 10),
              Btn(
                label: 'Все серверы · ${state.nodeCount}',
                icon: Icons.public,
                onPressed: () {
                  Navigator.pop(context);
                  onAll();
                },
              ),
            ],
          ),
        );
      },
    );
  }
}

class _FastestTile extends StatelessWidget {
  final AppState state;
  final (Subscription, NodeView)? fastest;
  final void Function(Subscription, NodeView) onPick;
  const _FastestTile({required this.state, required this.fastest, required this.onPick});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final f = fastest;
    final ms = f == null ? 0 : state.latencyOf(f.$1.id, f.$2.fingerprint)!.ms;
    return Material(
      color: accent.withValues(alpha: .10),
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: BorderSide(color: accent.withValues(alpha: .45)),
      ),
      clipBehavior: Clip.antiAlias,
      child: InkWell(
        onTap: f != null ? () => onPick(f.$1, f.$2) : (state.testingLatency || !state.online ? null : () => state.testLatency()),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
          child: Row(
            children: [
              Icon(Icons.bolt, color: p.accentInk, size: 22),
              const SizedBox(width: 12),
              Expanded(
                child: f != null
                    ? Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Text('Самый быстрый', style: TextStyle(fontWeight: FontWeight.w600)),
                          const SizedBox(height: 2),
                          Text(
                            cleanNodeName(f.$2.name),
                            style: TextStyle(fontSize: 12.5, color: p.muted),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ],
                      )
                    : Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          const Text('Найти самый быстрый', style: TextStyle(fontWeight: FontWeight.w600)),
                          const SizedBox(height: 2),
                          Text(
                            state.testingLatency ? 'Проверено ${state.latencyDone} из ${state.latencyTotal}' : 'Проверим пинг всех серверов',
                            style: TextStyle(fontSize: 12.5, color: p.muted),
                          ),
                        ],
                      ),
              ),
              if (f != null)
                Text('$ms мс', style: figures(15, color: ms < 200 ? okColor : (ms < 500 ? warnColor : errColor)))
              else if (state.testingLatency)
                const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
              else
                Icon(Icons.speed, color: p.muted, size: 20),
            ],
          ),
        ),
      ),
    );
  }
}

class _QuickRow extends StatelessWidget {
  final AppState state;
  final Subscription sub;
  final NodeView node;
  final VoidCallback onTap;
  const _QuickRow({required this.state, required this.sub, required this.node, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final sel = state.isSelected(sub, node);
    final l = state.latencyOf(sub.id, node.fingerprint);
    final unusable = node.cores.isEmpty;
    return Opacity(
      opacity: unusable ? .45 : 1,
      child: InkWell(
        borderRadius: BorderRadius.circular(10),
        onTap: unusable ? null : onTap,
        child: Container(
          constraints: const BoxConstraints(minHeight: 56),
          padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 8),
          decoration: BoxDecoration(color: sel ? accent.withValues(alpha: .10) : Colors.transparent, borderRadius: BorderRadius.circular(10)),
          child: Row(
            children: [
              CountryBadge(countryOf(node.name, node.server), width: 34),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Flexible(
                          child: Text(
                            cleanNodeName(node.name),
                            style: const TextStyle(fontWeight: FontWeight.w500),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                      ],
                    ),
                    const SizedBox(height: 3),
                    Row(
                      children: [
                        ProtoBadge(node.protocol),
                        const SizedBox(width: 6),
                        Flexible(
                          child: Text(
                            sub.displayName,
                            style: TextStyle(fontSize: 11.5, color: p.dim),
                            overflow: TextOverflow.ellipsis,
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
              if (l != null && l.ok) Text('${l.ms} мс', style: figures(13, color: l.ms < 200 ? okColor : (l.ms < 500 ? warnColor : errColor))),
              const SizedBox(width: 8),
              Icon(sel ? Icons.radio_button_checked : Icons.radio_button_off, size: 18, color: sel ? accent : p.dim),
            ],
          ),
        ),
      ),
    );
  }
}
