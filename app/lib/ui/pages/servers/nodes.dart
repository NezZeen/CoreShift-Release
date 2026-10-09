part of '../servers_page.dart';

class _NodeTable extends StatelessWidget {
  final AppState state;
  final List<_Section> sections;
  final Set<String> collapsed;
  final ValueChanged<String> onToggle;
  final _Picking picking;
  const _NodeTable({required this.state, required this.sections, required this.collapsed, required this.onToggle, required this.picking});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    if (sections.isEmpty) {
      return Padding(
        padding: const EdgeInsets.all(30),
        child: Center(
          child: Text('Ничего не найдено', style: TextStyle(color: p.dim)),
        ),
      );
    }
    final headStyle = TextStyle(fontSize: 12, color: p.dim, fontWeight: FontWeight.w600);
    // The columns need about 600 points; narrower, the rows become a list.
    return LayoutBuilder(
      builder: (context, c) {
        final narrow = isCompact(context) || c.maxWidth < 600;
        return Column(
          children: [
            _row(
              narrow: narrow,
              header: true,
              radio: const SizedBox(),
              badge: const SizedBox(),
              name: Text('Сервер', style: headStyle),
              proto: Text('Протокол', style: headStyle),
              ping: Tooltip(
                message: 'Зелёный — до 200 мс, жёлтый — до 500 мс',
                child: Text('Пинг', style: headStyle),
              ),
              action: const SizedBox(),
              tools: const SizedBox(),
            ),
            for (final (i, sec) in sections.indexed) ...[
              if (sec.title != null)
                _SectionHeading(
                  section: sec,
                  first: i == 0,
                  folded: collapsed.contains(sec.id),
                  onTap: sec.collapsible ? () => onToggle(sec.id) : null,
                  state: state,
                ),
              if (!collapsed.contains(sec.id))
                for (final (sub, n) in sec.rows)
                  _NodeRow(key: picking.rowKey(sub, n), state: state, sub: sub, node: n, showSub: sec.showSub, narrow: narrow, picking: picking),
            ],
          ],
        );
      },
    );
  }

  static Widget _row({
    required bool narrow,
    required Widget radio,
    required Widget badge,
    required Widget name,
    required Widget proto,
    required Widget ping,
    required Widget action,
    required Widget tools,
    bool header = false,
  }) {
    if (narrow) {
      // A phone: the name over the protocol, then the ping
      // and the actions; the column headings and the cores are left out.
      if (header) return const SizedBox.shrink();
      return Padding(
        padding: const EdgeInsets.only(left: 8, right: 2, top: 4, bottom: 4),
        child: Row(
          children: [
            // The radio, or the checkbox while picking: both at their own
            // size at the slot's left, clear of the flag.
            SizedBox(
              width: 22,
              child: Align(alignment: Alignment.centerLeft, child: radio),
            ),
            SizedBox(
              width: 38,
              child: Align(alignment: Alignment.centerLeft, child: badge),
            ),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                mainAxisSize: MainAxisSize.min,
                children: [
                  name,
                  const SizedBox(height: 4),
                  Align(alignment: Alignment.centerLeft, child: proto),
                ],
              ),
            ),
            SizedBox(width: 58, child: ping),
            action,
            tools,
          ],
        ),
      );
    }
    return Padding(
      padding: EdgeInsets.only(left: 12, right: 4, top: header ? 8 : 4, bottom: header ? 8 : 4),
      child: Row(
        children: [
          SizedBox(
            width: 28,
            child: Align(alignment: Alignment.centerLeft, child: radio),
          ),
          SizedBox(
            width: 42,
            child: Align(alignment: Alignment.centerLeft, child: badge),
          ),
          Expanded(flex: 14, child: name),
          Expanded(
            flex: 10,
            child: Align(alignment: Alignment.centerLeft, child: proto),
          ),
          SizedBox(width: 84, child: ping),
          // As tall as the "Подключить" button that shows under the pointer:
          // a row that grew when hovered would shift the rows below it and
          // jerk the list as the pointer moves.
          SizedBox(
            width: 118,
            height: header ? null : 28,
            child: Align(
              alignment: Alignment.centerRight,
              child: FittedBox(fit: BoxFit.scaleDown, child: action),
            ),
          ),
          // The star, always in its place.
          SizedBox(width: 36, height: header ? null : 28, child: tools),
        ],
      ),
    );
  }
}

/// A section's title: the favourites, a country with its best ping, a
/// subscription. A folding one is a tap target the width of the list.
class _SectionHeading extends StatelessWidget {
  final _Section section;
  final bool first;
  final bool folded;
  final VoidCallback? onTap;
  final AppState state;
  const _SectionHeading({required this.section, required this.first, required this.folded, required this.onTap, required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    var best = 1 << 30;
    for (final (sub, n) in section.rows) {
      final l = state.latencyOf(sub.id, n.fingerprint);
      if (l != null && l.ok && n.cores.isNotEmpty && l.ms < best) best = l.ms;
    }
    final content = Padding(
      padding: EdgeInsets.fromLTRB(12, first ? 6 : 14, 8, 6),
      child: LayoutBuilder(
        builder: (context, c) {
          final titleStyle = display(15, color: section.leading == null ? p.muted : p.text);
          final countStyle = TextStyle(fontSize: 12, color: p.dim);
          final pingStyle = figures(12.5, color: p.ink(best < 200 ? okColor : (best < 500 ? warnColor : errColor)));
          final count = '${section.rows.length}', ping = best < 1 << 30 ? 'от $best мс' : null;
          final scaler = MediaQuery.textScalerOf(context);
          double width(String s, TextStyle st) {
            final tp = TextPainter(
              text: TextSpan(text: s, style: st),
              textDirection: TextDirection.ltr,
              textScaler: scaler,
              maxLines: 1,
            )..layout();
            final w = tp.width;
            tp.dispose();
            return w;
          }

          // The title takes what it needs and the line the rest, so the fold
          // arrow stays at the right edge; a flexible title would split the
          // free width with the line and leave the arrow halfway. A long
          // title is cut where the line would get too short.
          final others =
              (section.leading != null ? 26 + 9 : 0) +
              8 +
              width(count, countStyle) +
              (ping != null ? 8 + width(ping, pingStyle) : 0) +
              10 +
              24 +
              (onTap != null ? 18 : 0);
          final titleWidth = min(width(section.title!, titleStyle) + 1, max(0.0, c.maxWidth - others));
          return Row(
            children: [
              if (section.leading != null) ...[section.leading!, const SizedBox(width: 9)],
              SizedBox(
                width: titleWidth,
                child: Text(section.title!, style: titleStyle, maxLines: 1, overflow: TextOverflow.ellipsis),
              ),
              const SizedBox(width: 8),
              Text(count, style: countStyle),
              if (ping != null) ...[const SizedBox(width: 8), Text(ping, style: pingStyle)],
              const SizedBox(width: 10),
              Expanded(child: Divider(height: 1, color: p.border)),
              if (onTap != null) Icon(folded ? Icons.expand_more : Icons.expand_less, size: 18, color: p.dim),
            ],
          );
        },
      ),
    );
    if (onTap == null) return content;
    return InkWell(borderRadius: BorderRadius.circular(10), onTap: onTap, child: content);
  }
}

class _NodeRow extends StatefulWidget {
  final AppState state;
  final Subscription sub;
  final NodeView node;
  final bool showSub;
  final bool narrow;
  final _Picking picking;
  const _NodeRow({super.key, required this.state, required this.sub, required this.node, required this.showSub, required this.narrow, required this.picking});

  @override
  State<_NodeRow> createState() => _NodeRowState();
}

class _NodeRowState extends State<_NodeRow> {
  bool hover = false;

  /// A right click's menu, at [at]: connect, the favourites, removal.
  Future<void> _menu(Offset at) async {
    final s = widget.state;
    final overlay = Overlay.of(context).context.findRenderObject() as RenderBox;
    final v = await showMenu<_NodeAction>(
      context: context,
      position: RelativeRect.fromRect(overlay.globalToLocal(at) & Size.zero, Offset.zero & overlay.size),
      constraints: const BoxConstraints(minWidth: 230),
      items: _serverMenuItems(s, widget.sub, widget.node),
    );
    if (mounted) setState(() => hover = false);
    if (v != null && mounted) _runServerAction(s, widget.sub, widget.node, v);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final s = widget.state;
    final n = widget.node;
    final pick = widget.picking;
    final picking = pick.active;
    final picked = picking && pick.isPicked(widget.sub, n);
    final sel = s.isSelected(widget.sub, n);
    final active = sel && s.status.active;
    final connected = sel && s.status.state == ConnState.connected;
    final unusable = n.cores.isEmpty;
    final narrow = widget.narrow;
    final compact = isCompact(context);
    final fav = s.isFavorite(widget.sub, n);
    void connect() => s.connect(subscription: widget.sub.id, fingerprint: n.fingerprint, name: n.name);

    final Widget action;
    if (connected) {
      action = narrow ? const Icon(Icons.check_circle, size: 18, color: okColor) : const Pill('Подключён', color: okColor);
    } else if (active) {
      action = narrow
          ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: accent))
          : Pill('Подключение…', color: p.accentInk);
    } else if (picking) {
      // Picking: a tap picks the row, so nothing on it connects.
      action = const SizedBox();
    } else if (!unusable && sel && narrow) {
      action = Btn(icon: Icons.power_settings_new, small: true, kind: BtnKind.primary, tooltip: 'Подключить', onPressed: s.busy || !s.online ? null : connect);
    } else if (!unusable && (hover || sel) && !narrow) {
      action = Btn(
        label: 'Подключить',
        icon: Icons.power_settings_new,
        small: true,
        kind: sel ? BtnKind.primary : BtnKind.normal,
        onPressed: s.busy || !s.online ? null : connect,
      );
    } else {
      action = const SizedBox();
    }

    // The star: always there, amber when the server is a favourite. A
    // phone's is a finger wide.
    final star = Tooltip(
      message: fav ? 'Убрать из избранного' : 'В избранное',
      child: InkResponse(
        onTap: picking ? null : () => s.toggleFavorite(widget.sub, n),
        radius: compact ? 22 : 16,
        child: SizedBox(
          width: compact ? 40 : 32,
          height: compact ? 40 : 28,
          child: Icon(fav ? Icons.star_rounded : Icons.star_outline_rounded, size: compact ? 22 : 19, color: fav ? warnColor : p.dim),
        ),
      ),
    );

    final row = _NodeTable._row(
      narrow: narrow,
      radio: picking ? _PickBox(on: picked, enabled: pick.canPick(widget.sub, n)) : _Radio(on: sel),
      badge: CountryBadge(countryOf(n.name, n.server), width: narrow ? 32 : 30),
      name: Tooltip(
        // A phone's long press picks the row.
        triggerMode: compact ? TooltipTriggerMode.manual : null,
        message:
            '${n.server}:${n.port}\n'
            '${n.cores.isEmpty ? 'Ни одно ядро не поддерживает' : 'Ядра: ${n.cores.map((k) => coreStyle(k).name).join(', ')}'}',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              cleanNodeName(n.name),
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
      proto: ProtoBadge(n.protocol),
      ping: _LatencyCell(latency: s.latencyOf(widget.sub.id, n.fingerprint), testing: s.testingLatency),
      action: action,
      tools: star,
    );

    final Color bg;
    if (picked) {
      bg = accent.withValues(alpha: .16);
    } else if (sel && !picking) {
      bg = accent.withValues(alpha: .10);
    } else {
      bg = hover && !unusable ? p.surface2 : Colors.transparent;
    }
    Widget body = Opacity(
      opacity: unusable && !picked ? .45 : 1,
      child: AnimatedContainer(
        duration: const Duration(milliseconds: 120),
        decoration: BoxDecoration(color: bg, borderRadius: BorderRadius.circular(10)),
        child: row,
      ),
    );

    if (narrow) {
      // A phone: swipe right to connect. The row stays where it is; the
      // swipe only triggers the action. Not while picking: the finger
      // picks then.
      body = Dismissible(
        key: ValueKey('swipe/${widget.sub.id}/${n.fingerprint}/${n.name}'),
        direction: unusable || picking ? DismissDirection.none : DismissDirection.startToEnd,
        dismissThresholds: const {DismissDirection.startToEnd: .28},
        confirmDismiss: (dir) async {
          s.setPref(serverHintPref, true);
          if (!s.busy && s.online) {
            HapticFeedback.selectionClick();
            connect();
          }
          return false;
        },
        background: _SwipeBackground(color: okColor, icon: Icons.power_settings_new, label: 'Подключить', alignLeft: true),
        child: body,
      );
    }

    return Tooltip(
      triggerMode: compact ? TooltipTriggerMode.manual : null,
      message: unusable && !picking ? 'Ни одно установленное ядро не поддерживает этот сервер' : '',
      child: MouseRegion(
        cursor: unusable && !picking ? SystemMouseCursors.basic : SystemMouseCursors.click,
        onEnter: (_) => setState(() => hover = true),
        onExit: (_) => setState(() => hover = false),
        child: GestureDetector(
          // A press held, then moved on: the rows it passes are picked.
          onLongPressStart: (d) => pick.start(widget.sub, n, d.globalPosition),
          onLongPressMoveUpdate: (d) => pick.move(d.globalPosition),
          onLongPressEnd: (_) => pick.end(),
          onSecondaryTapUp: picking ? null : (d) => _menu(d.globalPosition),
          onDoubleTap: picking || unusable || compact || s.busy || !s.online || (sel && s.status.active) ? null : connect,
          onTap: picking
              ? () => pick.toggle(widget.sub, n)
              : unusable || sel
              ? null
              : compact && s.status.active && s.online && !s.busy
              ? connect
              : () => s.selectNode(widget.sub.id, n.fingerprint, n.name),
          child: body,
        ),
      ),
    );
  }
}

class _SwipeBackground extends StatelessWidget {
  final Color color;
  final IconData icon;
  final String label;
  final bool alignLeft;
  const _SwipeBackground({required this.color, required this.icon, required this.label, required this.alignLeft});

  @override
  Widget build(BuildContext context) => Container(
    margin: const EdgeInsets.symmetric(vertical: 1),
    padding: const EdgeInsets.symmetric(horizontal: 18),
    alignment: alignLeft ? Alignment.centerLeft : Alignment.centerRight,
    decoration: BoxDecoration(color: color.withValues(alpha: .16), borderRadius: BorderRadius.circular(10)),
    child: Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (!alignLeft)
          Text(
            label,
            style: TextStyle(color: context.pal.ink(color), fontWeight: FontWeight.w600, fontSize: 13),
          ),
        if (!alignLeft) const SizedBox(width: 8),
        Icon(icon, color: context.pal.ink(color), size: 20),
        if (alignLeft) const SizedBox(width: 8),
        if (alignLeft)
          Text(
            label,
            style: TextStyle(color: context.pal.ink(color), fontWeight: FontWeight.w600, fontSize: 13),
          ),
      ],
    ),
  );
}

class _LatencyCell extends StatelessWidget {
  final Latency? latency;
  final bool testing;
  const _LatencyCell({required this.latency, required this.testing});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final l = latency;
    final mono = figures(13.5);
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
        child: Text('нет ответа', style: mono.copyWith(color: p.errInk, fontSize: 11)),
      );
    }
    final color = p.ink(l.ms < 200 ? okColor : (l.ms < 500 ? warnColor : errColor));
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
