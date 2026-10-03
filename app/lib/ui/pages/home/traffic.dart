part of '../home_page.dart';

const _weekdays = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'];
const _weekdaysLong = ['понедельник', 'вторник', 'среда', 'четверг', 'пятница', 'суббота', 'воскресенье'];

String _dayLabel(DateTime d) => '${_weekdaysLong[d.weekday - 1]}, ${d.day.toString().padLeft(2, '0')}.${d.month.toString().padLeft(2, '0')}';

/// The traffic through the VPN in one line: today's, and the week's in
/// bars behind «Подробнее».
class _TrafficRow extends StatelessWidget {
  final AppState state;
  const _TrafficRow({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final week = state.statsDays(7);
    final today = week.isEmpty ? TrafficDay(DateTime.now(), 0, 0) : week.last;
    final total = week.fold<int>(0, (n, d) => n + d.total);
    return _ToolRow(
      icon: Icons.bar_chart,
      title: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Text('Трафик', style: TextStyle(fontWeight: FontWeight.w600)),
          const SizedBox(width: 10),
          Flexible(
            child: FittedBox(
              fit: BoxFit.scaleDown,
              alignment: Alignment.centerLeft,
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Icon(Icons.south, size: 13, color: okColor),
                  Text(' ${formatBytes(today.down)}  ', style: figures(15)),
                  const Icon(Icons.north, size: 13, color: accent),
                  Text(' ${formatBytes(today.up)}', style: figures(15)),
                ],
              ),
            ),
          ),
        ],
      ),
      note: 'сегодня; за неделю ${formatBytes(total)}',
      noteColor: p.dim,
      action: Btn(label: 'Подробнее', small: true, onPressed: () => showTrafficStats(context, state)),
    );
  }
}

/// Bars per day: the download below in green, the upload above in blue.
/// Pointing at a bar, or tapping it, shows that day under the chart.
class _TrafficChart extends StatefulWidget {
  final List<TrafficDay> days;

  /// A sparkline: no labels, no picking. Not used since the phone shows
  /// the full card too.
  final bool mini = false;
  const _TrafficChart({required this.days});

  @override
  State<_TrafficChart> createState() => _TrafficChartState();
}

class _TrafficChartState extends State<_TrafficChart> {
  int? picked;

  int _at(double x, double width) => ((x / width) * widget.days.length).floor().clamp(0, widget.days.length - 1);

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final days = widget.days;
    if (days.isEmpty) return const SizedBox();
    final chart = LayoutBuilder(
      builder: (context, c) {
        final paint = CustomPaint(
          painter: _ChartPainter(
            days,
            picked: widget.mini ? null : picked,
            grid: p.border,
            label: p.dim,
            mini: widget.mini,
            dayLabel: days.length <= 7 ? (d) => _weekdays[d.weekday - 1] : (d) => '${d.day}',
            base: Theme.of(context).textTheme.bodySmall ?? const TextStyle(),
          ),
          size: Size.infinite,
        );
        if (widget.mini) return paint;
        return MouseRegion(
          onHover: (e) => setState(() => picked = _at(e.localPosition.dx, c.maxWidth)),
          onExit: (_) => setState(() => picked = null),
          child: GestureDetector(onTapDown: (d) => setState(() => picked = _at(d.localPosition.dx, c.maxWidth)), child: paint),
        );
      },
    );
    if (widget.mini) return chart;
    final shown = days[picked ?? days.length - 1];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Expanded(child: chart),
        const SizedBox(height: 6),
        Text(
          '${picked == null ? 'Сегодня' : _dayLabel(shown.date)}: ↓ ${formatBytes(shown.down)}   ↑ ${formatBytes(shown.up)}',
          style: TextStyle(fontSize: 12, color: p.muted),
        ),
      ],
    );
  }
}

class _ChartPainter extends CustomPainter {
  final List<TrafficDay> days;
  final int? picked;
  final Color grid;
  final Color label;
  final bool mini;
  final String Function(DateTime) dayLabel;

  /// The page's text style, so the labels use its font.
  final TextStyle base;
  _ChartPainter(this.days, {required this.picked, required this.grid, required this.label, required this.mini, required this.dayLabel, required this.base});

  @override
  void paint(Canvas canvas, Size size) {
    final labelH = mini ? 0.0 : 16.0;
    final h = size.height - labelH;
    final peak = days.fold<int>(0, (m, d) => max(m, d.total));
    final maxV = max(peak, 1 << 20).toDouble(); // at least 1 MB tall
    final slot = size.width / days.length;
    final gap = mini ? 2.0 : max(3.0, slot * .22);
    final bw = max(2.0, slot - gap);
    if (!mini) canvas.drawLine(Offset(0, h), Offset(size.width, h), Paint()..color = grid);
    for (final (i, d) in days.indexed) {
      final x = i * slot + gap / 2;
      final today = i == days.length - 1;
      final alpha = mini || picked == null ? (today ? 1.0 : .75) : (picked == i ? 1.0 : .45);
      final hd = h * d.down / maxV, hu = h * d.up / maxV;
      final r = Radius.circular(min(3.0, bw / 2));
      // A bar with nothing in it is a thin stub, so the day is not missing.
      if (d.total == 0) {
        canvas.drawRRect(RRect.fromRectAndRadius(Rect.fromLTWH(x, h - 2, bw, 2), r), Paint()..color = grid);
      } else {
        canvas.drawRRect(
          RRect.fromRectAndCorners(
            Rect.fromLTWH(x, h - hd, bw, max(hd, 1)),
            bottomLeft: r,
            bottomRight: r,
            topLeft: hu < 1 ? r : Radius.zero,
            topRight: hu < 1 ? r : Radius.zero,
          ),
          Paint()..color = okColor.withValues(alpha: alpha),
        );
        if (hu >= 1) {
          canvas.drawRRect(
            RRect.fromRectAndCorners(Rect.fromLTWH(x, h - hd - hu, bw, hu), topLeft: r, topRight: r),
            Paint()..color = accent.withValues(alpha: alpha),
          );
        }
      }
      if (!mini && (days.length <= 7 || i % 5 == 4 || today)) {
        final tp = TextPainter(
          text: TextSpan(
            text: dayLabel(d.date),
            style: base.copyWith(color: label, fontSize: 10, fontWeight: today || picked == i ? FontWeight.w700 : FontWeight.w400),
          ),
          textDirection: TextDirection.ltr,
        )..layout();
        tp.paint(canvas, Offset(x + bw / 2 - tp.width / 2, h + 3));
      }
    }
  }

  @override
  bool shouldRepaint(_ChartPainter old) => true;
}

/// The week's traffic in detail: the totals, the average and the busiest day.
/// A window on the desktop, a sheet on a phone.
Future<void> showTrafficStats(BuildContext context, AppState state) {
  final p = context.pal;
  if (isCompact(context)) {
    return showModalBottomSheet<void>(
      context: context,
      backgroundColor: p.surface,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (c) => SafeArea(child: _TrafficDetails(state: state)),
    );
  }
  return showDialog<void>(
    context: context,
    builder: (c) => Dialog(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560),
        child: _TrafficDetails(state: state),
      ),
    ),
  );
}

/// The last week only: what is useful to know about one's traffic.
class _TrafficDetails extends StatelessWidget {
  final AppState state;
  const _TrafficDetails({required this.state});

  @override
  Widget build(BuildContext context) {
    final compact = isCompact(context);
    return ListenableBuilder(
      listenable: state,
      builder: (context, _) {
        final p = context.pal;
        final days = state.statsDays(7);
        final down = days.fold<int>(0, (n, d) => n + d.down);
        final up = days.fold<int>(0, (n, d) => n + d.up);
        final busiest = days.isEmpty ? null : days.reduce((a, b) => a.total >= b.total ? a : b);
        final active = days.where((d) => d.total > 0).length;
        Widget stat(String label, String value, [String note = '']) => Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(label, style: TextStyle(fontSize: 11, color: p.muted)),
              const SizedBox(height: 2),
              Text(value, style: figures(19, weight: FontWeight.w600)),
              if (note.isNotEmpty) Text(note, style: TextStyle(fontSize: 11.5, color: p.dim)),
            ],
          ),
        );
        return SingleChildScrollView(
          padding: EdgeInsets.fromLTRB(compact ? 16 : 22, compact ? 0 : 20, compact ? 16 : 22, 20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('Трафик через VPN', style: dialogTitle),
              const SizedBox(height: 2),
              Text('за 7 дней', style: TextStyle(fontSize: 12.5, color: p.muted)),
              const SizedBox(height: 16),
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  stat('Скачано', formatBytes(down)),
                  stat('Отправлено', formatBytes(up)),
                  stat('В день', active == 0 ? '—' : formatBytes((down + up) / active), active == 0 ? '' : 'в среднем'),
                ],
              ),
              const SizedBox(height: 16),
              SizedBox(height: 150, child: _TrafficChart(days: days)),
              if (busiest != null && busiest.total > 0) ...[
                const SizedBox(height: 12),
                Text('Больше всего: ${_dayLabel(busiest.date)}, ${formatBytes(busiest.total)}', style: TextStyle(fontSize: 12.5, color: p.muted)),
              ],
              const SizedBox(height: 4),
              Text('Считается только трафик, прошедший через VPN. История хранится 90 дней.', style: TextStyle(fontSize: 11.5, color: p.dim)),
              if (!compact) ...[
                const SizedBox(height: 14),
                Align(
                  alignment: Alignment.centerRight,
                  child: Btn(label: 'Закрыть', onPressed: () => Navigator.pop(context)),
                ),
              ],
            ],
          ),
        );
      },
    );
  }
}
