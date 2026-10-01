part of '../servers_page.dart';

class _NodeTable extends StatelessWidget {
  final AppState state;
  final List<_Section> sections;
  final Set<String> collapsed;
  final ValueChanged<String> onToggle;
  const _NodeTable({required this.state, required this.sections, required this.collapsed, required this.onToggle});

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
              radio: const SizedBox(),
              badge: const SizedBox(),
              name: Text('СЕРВЕР', style: headStyle),
              proto: Text('ПРОТОКОЛ', style: headStyle),
              ping: Tooltip(
                message: 'Зелёный — до 200 мс, жёлтый — до 500 мс',
                child: Text('ПИНГ', style: headStyle),
              ),
              action: const SizedBox(),
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
                  _NodeRow(key: ValueKey('${sub.id}/${n.fingerprint}/${n.name}'), state: state, sub: sub, node: n, showSub: sec.showSub, narrow: narrow),
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
            SizedBox(width: 22, child: radio),
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
          ],
        ),
      );
    }
    return Padding(
      padding: EdgeInsets.only(left: 12, right: 4, top: header ? 8 : 6, bottom: header ? 8 : 6),
      child: Row(
        children: [
          SizedBox(width: 28, child: radio),
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
          SizedBox(
            width: 118,
            child: Align(
              alignment: Alignment.centerRight,
              child: FittedBox(fit: BoxFit.scaleDown, child: action),
            ),
          ),
        ],
      ),
    );
  }
}

/// A section's title: a country with its best ping, a
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
      child: Row(
        children: [
          if (section.leading != null) ...[section.leading!, const SizedBox(width: 9)],
          Flexible(
            child: Text(
              section.title!,
              style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: section.leading == null ? p.muted : p.text),
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const SizedBox(width: 8),
          Text('${section.rows.length}', style: TextStyle(fontSize: 12, color: p.dim)),
          if (best < 1 << 30) ...[
            const SizedBox(width: 8),
            Text(
              'от $best мс',
              style: TextStyle(
                fontSize: 11.5,
                color: best < 200 ? okColor : (best < 500 ? warnColor : errColor),
                fontFamily: monoFont,
                fontFamilyFallback: monoFallback,
              ),
            ),
          ],
          const SizedBox(width: 10),
          Expanded(child: Divider(height: 1, color: p.border)),
          if (onTap != null) Icon(folded ? Icons.expand_more : Icons.expand_less, size: 18, color: p.dim),
        ],
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
  const _NodeRow({super.key, required this.state, required this.sub, required this.node, required this.showSub, required this.narrow});

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
    final narrow = widget.narrow;
    void connect() => s.connect(subscription: widget.sub.id, fingerprint: n.fingerprint, name: n.name);

    final Widget action;
    if (connected) {
      action = narrow ? const Icon(Icons.check_circle, size: 18, color: okColor) : const Pill('ПОДКЛЮЧЁН', color: okColor);
    } else if (active) {
      action = narrow
          ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2, color: accent))
          : const Pill('ПОДКЛЮЧЕНИЕ…', color: accent);
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

    final row = _NodeTable._row(
      narrow: narrow,
      radio: _Radio(on: sel),
      badge: CountryBadge(countryOf(n.name, n.server), width: narrow ? 32 : 30),
      name: Tooltip(
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
    );

    Widget body = Opacity(
      opacity: unusable ? .45 : 1,
      child: Container(
        decoration: BoxDecoration(
          color: sel ? accent.withValues(alpha: .10) : (hover && !unusable ? p.surface2 : Colors.transparent),
          borderRadius: BorderRadius.circular(10),
        ),
        child: row,
      ),
    );

    if (narrow) {
      // A phone: swipe right to connect. The row stays where it is; the
      // swipe only triggers the action.
      body = Dismissible(
        key: ValueKey('swipe/${widget.sub.id}/${n.fingerprint}/${n.name}'),
        direction: unusable ? DismissDirection.none : DismissDirection.startToEnd,
        dismissThresholds: const {DismissDirection.startToEnd: .28},
        confirmDismiss: (dir) async {
          s.setPref('swipe_hint', true);
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
      message: unusable ? 'Ни одно установленное ядро не поддерживает этот сервер' : '',
      child: MouseRegion(
        cursor: unusable ? SystemMouseCursors.basic : SystemMouseCursors.click,
        onEnter: (_) => setState(() => hover = true),
        onExit: (_) => setState(() => hover = false),
        child: GestureDetector(
          onDoubleTap: unusable || isCompact(context) || s.busy || !s.online || (sel && s.status.active) ? null : connect,
          onTap: unusable || sel
              ? null
              : isCompact(context) && s.status.active && s.online && !s.busy
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
            style: TextStyle(color: color, fontWeight: FontWeight.w600, fontSize: 13),
          ),
        if (!alignLeft) const SizedBox(width: 8),
        Icon(icon, color: color, size: 20),
        if (alignLeft) const SizedBox(width: 8),
        if (alignLeft)
          Text(
            label,
            style: TextStyle(color: color, fontWeight: FontWeight.w600, fontSize: 13),
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
