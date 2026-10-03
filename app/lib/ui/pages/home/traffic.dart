part of '../home_page.dart';

const _weekdays = ['Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб', 'Вс'];
const _weekdaysLong = ['понедельник', 'вторник', 'среда', 'четверг', 'пятница', 'суббота', 'воскресенье'];

String _dayLabel(DateTime d) => '${_weekdaysLong[d.weekday - 1]}, ${d.day.toString().padLeft(2, '0')}.${d.month.toString().padLeft(2, '0')}';

/// Today, the week and the way into the details: the traffic through the
/// VPN, with the week's bars.
class _TrafficCard extends StatelessWidget {
  final AppState state;
  const _TrafficCard({required this.state});

  @override
  Widget build(BuildContext context) {
    WidgetsBinding.instance.addPostFrameCallback((_) => state.watchStats());
    if (state.statsUnsupported || !state.statsLoaded) return const SizedBox();
    final week = state.statsDays(7);
    final today = week.isEmpty ? TrafficDay(DateTime.now(), 0, 0) : week.last;
    void open() => showTrafficStats(context, state);
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.only(bottom: 14),
            child: Row(
              children: [
                Text('Трафик', style: display(16)),
                if (!isCompact(context)) ...[
                  const SizedBox(width: 10),
                  Flexible(
                    child: Text(
                      'через VPN, за 7 дней',
                      style: TextStyle(color: context.pal.muted, fontSize: 12),
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
                const Spacer(),
                Btn(label: 'Подробнее', small: true, onPressed: open),
              ],
            ),
          ),
          Wrap(
            spacing: 32,
            runSpacing: 10,
            children: [
              _Total(label: 'Сегодня', down: today.down, up: today.up),
              _Total(label: 'За 7 дней', down: week.fold(0, (n, d) => n + d.down), up: week.fold(0, (n, d) => n + d.up)),
            ],
          ),
          const SizedBox(height: 14),
          SizedBox(height: 96, child: _TrafficChart(days: week)),
        ],
      ),
    );
  }
}

class _Amount extends StatelessWidget {
  final IconData icon;
  final Color color;
  final int bytes;
  const _Amount({required this.icon, required this.color, required this.bytes});

  @override
  Widget build(BuildContext context) => Row(
    mainAxisSize: MainAxisSize.min,
    children: [
      Icon(icon, size: 14, color: color),
      const SizedBox(width: 4),
      Text(formatBytes(bytes), style: figures(17)),
    ],
  );
}

class _Total extends StatelessWidget {
  final String label;
  final int down;
  final int up;
  const _Total({required this.label, required this.down, required this.up});

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(label, style: TextStyle(fontSize: 11, color: context.pal.muted)),
      const SizedBox(height: 2),
      Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          _Amount(icon: Icons.south, color: okColor, bytes: down),
          const SizedBox(width: 14),
          _Amount(icon: Icons.north, color: accent, bytes: up),
        ],
      ),
    ],
  );
}

/// Bars per day: the download below in green, the upload above in blue.
/// Pointing at a bar, or tapping it, shows that day under the chart.
class _TrafficChart extends StatefulWidget {
  final List<TrafficDay> days;

  /// A sparkline: no labels, no picking. Not used since the phone shows
  /// the full card too.
  final bool mini = false;
  const _TrafficChart({super.key, required this.days});

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

/// The traffic in detail: a week or a month, the totals and the busiest day.
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

class _TrafficDetails extends StatefulWidget {
  final AppState state;
  const _TrafficDetails({required this.state});

  @override
  State<_TrafficDetails> createState() => _TrafficDetailsState();
}

class _TrafficDetailsState extends State<_TrafficDetails> {
  int span = 7;

  @override
  Widget build(BuildContext context) {
    final compact = isCompact(context);
    return ListenableBuilder(
      listenable: widget.state,
      builder: (context, _) {
        final p = context.pal;
        final days = widget.state.statsDays(span);
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
              Row(
                children: [
                  Expanded(child: Text('Трафик через VPN', style: dialogTitle)),
                  Seg<int>(value: span, options: const [(7, '7 дней'), (30, '30 дней')], onChanged: (v) => setState(() => span = v)),
                ],
              ),
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
              SizedBox(
                height: 150,
                child: _TrafficChart(key: ValueKey(span), days: days),
              ),
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
