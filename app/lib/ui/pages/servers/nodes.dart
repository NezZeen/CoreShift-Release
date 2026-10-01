part of '../servers_page.dart';

class _NodeTable extends StatelessWidget {
  final AppState state;
  final List<(Subscription, NodeView)> rows;
  final bool showSub;
  const _NodeTable({required this.state, required this.rows, required this.showSub});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    if (rows.isEmpty) {
      return Padding(
        padding: const EdgeInsets.all(30),
        child: Center(
          child: Text('Ничего не найдено', style: TextStyle(color: p.dim)),
        ),
      );
    }
    final headStyle = TextStyle(fontSize: 11, color: p.dim, letterSpacing: .6, fontWeight: FontWeight.w600);
    // The columns need about 640 points; narrower, the rows become a list.
    return LayoutBuilder(
      builder: (context, c) {
        final narrow = isCompact(context) || c.maxWidth < 640;
        return Column(
          children: [
            _row(
              narrow: narrow,
              header: true,
              cells: [
                const SizedBox(),
                Text('СЕРВЕР', style: headStyle),
                Text('ПРОТОКОЛ', style: headStyle),
                Text('ТРАНСПОРТ', style: headStyle),
                Tooltip(
                  message: 'Зелёный — до 200 мс, жёлтый — до 500 мс',
                  child: Text('ПИНГ', style: headStyle),
                ),
                const SizedBox(),
              ],
            ),
            for (final (i, (sub, n)) in rows.indexed) ...[
              // With every subscription shown, each gets a heading rather
              // than its name under each of its servers.
              if (showSub && (i == 0 || rows[i - 1].$1.id != sub.id)) _SubHeading(sub: sub, count: rows.where((r) => r.$1.id == sub.id).length, first: i == 0),
              _NodeRow(state: state, sub: sub, node: n, showSub: false, narrow: narrow),
            ],
          ],
        );
      },
    );
  }

  static Widget _row({required bool narrow, required List<Widget> cells, bool header = false}) {
    if (narrow) {
      // A phone: the name over the protocol and transport, then the ping
      // and the action; the column headings and the cores are left out.
      if (header) return const SizedBox.shrink();
      return Padding(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 10),
        child: Row(
          children: [
            SizedBox(width: 28, child: cells[0]),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  cells[1],
                  const SizedBox(height: 4),
                  Row(
                    children: [
                      cells[2],
                      const SizedBox(width: 8),
                      Flexible(child: cells[3]),
                    ],
                  ),
                ],
              ),
            ),
            SizedBox(width: 64, child: cells[4]),
            ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 104),
              child: FittedBox(fit: BoxFit.scaleDown, child: cells[5]),
            ),
          ],
        ),
      );
    }
    return Padding(
      padding: EdgeInsets.symmetric(horizontal: 12, vertical: header ? 8 : 10),
      child: Row(
        children: [
          SizedBox(width: 28, child: cells[0]),
          Expanded(flex: 14, child: cells[1]),
          SizedBox(width: 118, child: cells[2]),
          Expanded(flex: 10, child: cells[3]),
          SizedBox(width: 84, child: cells[4]),
          SizedBox(
            width: 118,
            child: Align(
              alignment: Alignment.centerRight,
              child: FittedBox(fit: BoxFit.scaleDown, child: cells[5]),
            ),
          ),
        ],
      ),
    );
  }
}

class _SubHeading extends StatelessWidget {
  final Subscription sub;
  final int count;
  final bool first;
  const _SubHeading({required this.sub, required this.count, required this.first});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Padding(
      padding: EdgeInsets.fromLTRB(12, first ? 4 : 16, 12, 6),
      child: Row(
        children: [
          Flexible(
            child: Text(
              sub.displayName,
              style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: p.muted),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(width: 8),
          Text('$count', style: TextStyle(fontSize: 12, color: p.dim)),
          const SizedBox(width: 10),
          Expanded(child: Divider(height: 1, color: p.border)),
        ],
      ),
    );
  }
}

class _NodeRow extends StatefulWidget {
  final AppState state;
  final Subscription sub;
  final NodeView node;
  final bool showSub;
  final bool narrow;
  const _NodeRow({required this.state, required this.sub, required this.node, required this.showSub, required this.narrow});

  @override
  State<_NodeRow> createState() => _NodeRowState();
}

class _NodeRowState extends State<_NodeRow> {
  bool hover = false;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final s = widget.state;
    final n = widget.node;
    final sel = s.isSelected(widget.sub, n);
    final active = sel && s.status.active;
    final connected = sel && s.status.state == ConnState.connected;
    final unusable = n.cores.isEmpty;

    final row = _NodeTable._row(
      narrow: widget.narrow,
      cells: [
        _Radio(on: sel),
        Row(
          children: [
            Flexible(
              child: Tooltip(
                message:
                    '${n.server}:${n.port}\n'
                    '${n.cores.isEmpty ? 'Ни одно ядро не поддерживает' : 'Ядра: ${n.cores.map((k) => coreStyle(k).name).join(', ')}'}',
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      n.name,
                      style: const TextStyle(fontWeight: FontWeight.w500),
                      overflow: TextOverflow.ellipsis,
                    ),
                    if (widget.showSub)
                      Text(
                        widget.sub.displayName,
                        style: TextStyle(fontSize: 11, color: p.dim),
                        overflow: TextOverflow.ellipsis,
                      ),
                  ],
                ),
              ),
            ),
          ],
        ),
        Align(alignment: Alignment.centerLeft, child: ProtoBadge(n.protocol)),
        Text(
          '${n.transport} · ${n.security}',
          style: TextStyle(fontSize: 12, color: p.muted),
          overflow: TextOverflow.ellipsis,
        ),
        _LatencyCell(latency: s.latencyOf(widget.sub.id, n.fingerprint), testing: s.testingLatency),
        if (connected)
          const Pill('ПОДКЛЮЧЁН', color: okColor)
        else if (active)
          const Pill('ПОДКЛЮЧЕНИЕ…', color: accent)
        else if (!unusable && (hover || sel))
          Btn(
            label: 'Подключить',
            icon: Icons.power_settings_new,
            small: true,
            kind: sel ? BtnKind.primary : BtnKind.normal,
            onPressed: s.busy || !s.online ? null : () => s.connect(subscription: widget.sub.id, fingerprint: n.fingerprint, name: n.name),
          )
        else
          const SizedBox(),
      ],
    );

    return Tooltip(
      message: unusable ? 'Ни одно установленное ядро не поддерживает этот сервер' : '',
      child: MouseRegion(
        cursor: unusable ? SystemMouseCursors.basic : SystemMouseCursors.click,
        onEnter: (_) => setState(() => hover = true),
        onExit: (_) => setState(() => hover = false),
        child: GestureDetector(
          onDoubleTap: unusable || isCompact(context) || s.busy || !s.online || (sel && s.status.active)
              ? null
              : () => s.connect(subscription: widget.sub.id, fingerprint: n.fingerprint, name: n.name),
          onTap: unusable || sel
              ? null
              : isCompact(context) && s.status.active && s.online && !s.busy
              ? () => s.connect(subscription: widget.sub.id, fingerprint: n.fingerprint, name: n.name)
              : () => s.selectNode(widget.sub.id, n.fingerprint, n.name),
          child: Opacity(
            opacity: unusable ? .45 : 1,
            child: Container(
              decoration: BoxDecoration(
                color: sel ? accent.withValues(alpha: .10) : (hover && !unusable ? p.surface2 : Colors.transparent),
                borderRadius: BorderRadius.circular(10),
              ),
              child: row,
            ),
          ),
        ),
      ),
    );
  }
}

class _LatencyCell extends StatelessWidget {
  final Latency? latency;
  final bool testing;
  const _LatencyCell({required this.latency, required this.testing});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final l = latency;
    const mono = TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 12, fontWeight: FontWeight.w500);
    if (l == null) {
      return testing
          ? Align(
              alignment: Alignment.centerLeft,
              child: SizedBox(width: 12, height: 12, child: CircularProgressIndicator(strokeWidth: 1.5, color: p.dim)),
            )
          : Text('—', style: mono.copyWith(color: p.dim));
    }
    if (!l.ok) {
      return Tooltip(
        message: l.error,
        child: Text('нет ответа', style: mono.copyWith(color: errColor, fontSize: 11)),
      );
    }
    final color = l.ms < 200 ? okColor : (l.ms < 500 ? warnColor : errColor);
    final text = Text('${l.ms} мс', style: mono.copyWith(color: color));
    final how = switch (l.method) {
      'icmp' => 'ICMP-пинг до сервера',
      'tcp' => 'Время TCP-подключения к серверу',
      _ when l.core.isNotEmpty => 'Запрос через ${coreStyle(l.core).name}',
      _ => '',
    };
    return how.isEmpty ? text : Tooltip(message: how, child: text);
  }
}

class _Radio extends StatelessWidget {
  final bool on;
  const _Radio({required this.on});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      width: 16,
      height: 16,
      decoration: BoxDecoration(
        shape: BoxShape.circle,
        border: Border.all(color: on ? accent : p.border2, width: 2),
      ),
      padding: const EdgeInsets.all(2),
      child: on
          ? Container(
              decoration: const BoxDecoration(color: accent, shape: BoxShape.circle),
            )
          : null,
    );
  }
}
