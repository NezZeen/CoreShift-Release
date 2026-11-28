part of '../shell.dart';

/// The phone's navigation: the five main pages as icons on a pill that
/// floats above the bottom edge; the current one lit amber. The names are
/// the icons' tooltips (a long press) and what a screen reader says. On the
/// cores the settings stay lit.
class _BottomNav extends StatelessWidget {
  final AppState state;
  final PageId page;
  final ValueChanged<PageId> onPage;
  const _BottomNav({required this.state, required this.page, required this.onPage});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final current = stationOf(page);
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 6, 16, 12),
        child: Container(
          key: const ValueKey('pill-nav'),
          height: 64,
          decoration: BoxDecoration(
            color: p.surface,
            borderRadius: BorderRadius.circular(32),
            border: Border.all(color: p.border),
          ),
          child: Row(
            mainAxisAlignment: MainAxisAlignment.spaceAround,
            children: [for (final (id, icon, label) in mainPages) _PillTab(icon: icon, label: tr(label), active: current == id, onTap: () => onPage(id))],
          ),
        ),
      ),
    );
  }
}

class _PillTab extends StatelessWidget {
  final IconData icon;
  final String label;
  final bool active;
  final VoidCallback onTap;
  const _PillTab({required this.icon, required this.label, required this.active, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Tooltip(
      message: label,
      child: Semantics(
        button: true,
        selected: active,
        label: label,
        excludeSemantics: true,
        child: InkResponse(
          onTap: onTap,
          radius: 28,
          hoverColor: Colors.transparent,
          highlightColor: Colors.transparent,
          splashColor: accent.withValues(alpha: .18),
          highlightShape: BoxShape.rectangle,
          borderRadius: BorderRadius.circular(24),
          child: AnimatedContainer(
            duration: const Duration(milliseconds: 160),
            width: 56,
            height: 48,
            decoration: BoxDecoration(color: active ? accent.withValues(alpha: .14) : Colors.transparent, borderRadius: BorderRadius.circular(24)),
            child: Icon(icon, size: 24, color: active ? p.accentInk : p.dim),
          ),
        ),
      ),
    );
  }
}

class _Sidebar extends StatelessWidget {
  final AppState state;
  final PageId page;
  final ValueChanged<PageId> onPage;
  const _Sidebar({required this.state, required this.page, required this.onPage});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Container(
      width: 228,
      decoration: BoxDecoration(
        color: p.bg2,
        border: Border(right: BorderSide(color: p.border)),
      ),
      padding: const EdgeInsets.fromLTRB(12, 18, 12, 14),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(6, 0, 6, 18),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.center,
              children: [
                const CoreShiftMark(size: 26),
                const SizedBox(width: 10),
                Flexible(
                  child: Text('CoreShift', style: display(19, spacing: -.2), overflow: TextOverflow.ellipsis),
                ),
                const SizedBox(width: 6),
                Tooltip(
                  message: state.versionMismatch
                      ? state.versionMismatchAdvice
                      : state.version.commit.isEmpty
                      ? tr('shell.nav.version', {'label': state.version.label})
                      : tr('shell.nav.versionCommit', {'label': state.version.label, 'commit': state.version.commit}),
                  child: Text(
                    state.version.known ? state.version.version : state.info.version,
                    style: TextStyle(color: state.versionMismatch ? p.warnInk : p.dim, fontSize: 11, fontWeight: FontWeight.w500),
                  ),
                ),
              ],
            ),
          ),
          _StatusPill(state: state),
          const SizedBox(height: 18),
          // The pages as stations on one line.
          for (final (i, (id, icon, label)) in mainPages.indexed)
            _NavItem(icon: icon, label: tr(label), active: stationOf(page) == id, first: i == 0, last: i == mainPages.length - 1, onTap: () => onPage(id)),
          const Spacer(),
        ],
      ),
    );
  }
}

class _StatusPill extends StatelessWidget {
  final AppState state;
  const _StatusPill({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status.state;
    final (color, text) = !state.online
        ? (errColor, tr('shell.pill.noService'))
        : switch (st) {
            ConnState.connected => (okColor, tr('shell.pill.connected')),
            ConnState.connecting => (warnColor, tr('shell.pill.connecting')),
            ConnState.disconnecting => (warnColor, tr('shell.pill.disconnecting')),
            ConnState.failed => (errColor, tr('shell.pill.failed')),
            ConnState.blocked => (errColor, tr('shell.pill.blocked')),
            ConnState.noNetwork => (warnColor, state.status.waiting ? tr('shell.pill.waitingNetwork') : tr('shell.pill.noNetwork')),
            ConnState.idle => (p.dim, tr('shell.pill.disconnected')),
          };
    final active = state.status.active;
    final canToggle = state.online && (active || (state.selection.available && !state.busy));
    final server = cleanNodeName(active ? state.status.node : state.selection.name);
    final on = st == ConnState.connected && state.online;
    return Container(
      padding: const EdgeInsets.fromLTRB(12, 8, 4, 8),
      decoration: BoxDecoration(
        color: on ? Color.alphaBlend(okColor.withValues(alpha: .12), p.surface) : p.surface,
        borderRadius: BorderRadius.circular(11),
        border: Border.all(color: on ? okColor.withValues(alpha: .5) : p.border),
      ),
      child: Row(
        children: [
          Lamp(color: color, lit: on || st == ConnState.connecting || st == ConnState.noNetwork, size: 9),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  text,
                  style: TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600, color: on ? p.okInk : p.text),
                  overflow: TextOverflow.ellipsis,
                ),
                if (state.online && server.isNotEmpty)
                  Text(
                    server,
                    style: TextStyle(fontSize: 11.5, color: p.muted),
                    overflow: TextOverflow.ellipsis,
                  ),
              ],
            ),
          ),
          IconButton(
            tooltip: active ? tr('shell.pill.disconnectTip') : tr('shell.pill.connectTip'),
            onPressed: canToggle ? state.toggleConnect : null,
            icon: Icon(Icons.power_settings_new, size: 18, color: active ? okColor : p.accentInk),
            visualDensity: VisualDensity.compact,
          ),
        ],
      ),
    );
  }
}

class _NavItem extends StatefulWidget {
  final IconData icon;
  final String label;
  final bool active;
  final bool first;
  final bool last;
  final VoidCallback onTap;
  const _NavItem({required this.icon, required this.label, required this.active, required this.onTap, this.first = false, this.last = false});

  @override
  State<_NavItem> createState() => _NavItemState();
}

class _NavItemState extends State<_NavItem> {
  bool hover = false;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final fg = widget.active || hover ? p.text : p.muted;
    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onEnter: (_) => setState(() => hover = true),
      onExit: (_) => setState(() => hover = false),
      child: GestureDetector(
        onTap: widget.onTap,
        behavior: HitTestBehavior.opaque,
        child: CustomPaint(
          painter: _StationPainter(active: widget.active, first: widget.first, last: widget.last, line: p.border2, hollow: p.bg2, hover: hover),
          child: Padding(
            padding: const EdgeInsets.only(left: 30),
            child: Container(
              margin: const EdgeInsets.symmetric(vertical: 2),
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
              decoration: BoxDecoration(
                color: widget.active ? p.surface : (hover ? p.surface.withValues(alpha: .6) : Colors.transparent),
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: widget.active ? p.border : Colors.transparent),
              ),
              child: Row(
                children: [
                  Icon(widget.icon, size: 17, color: widget.active ? p.accentInk : fg),
                  const SizedBox(width: 11),
                  Expanded(
                    child: Text(
                      widget.label,
                      style: TextStyle(color: fg, fontWeight: widget.active ? FontWeight.w600 : FontWeight.w500),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// A page's station on the sidebar's line: a hollow ring, the current page
/// a lit amber one.
class _StationPainter extends CustomPainter {
  final bool active, first, last, hover;
  final Color line, hollow;
  _StationPainter({required this.active, required this.first, required this.last, required this.line, required this.hollow, required this.hover});

  @override
  void paint(Canvas canvas, Size size) {
    const x = 12.0;
    final y = size.height / 2;
    final track = Paint()
      ..color = line
      ..strokeWidth = 2;
    canvas.drawLine(Offset(x, first ? y : 0), Offset(x, last ? y : size.height), track);
    if (active) {
      canvas.drawCircle(Offset(x, y), 10, Paint()..color = accent.withValues(alpha: .18));
      canvas.drawCircle(Offset(x, y), 6, Paint()..color = accent);
      return;
    }
    canvas.drawCircle(Offset(x, y), 5, Paint()..color = hollow);
    canvas.drawCircle(
      Offset(x, y),
      5,
      Paint()
        ..color = hover ? accent : line
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2,
    );
  }

  @override
  bool shouldRepaint(_StationPainter old) => old.active != active || old.hover != hover || old.line != line || old.hollow != hollow;
}
