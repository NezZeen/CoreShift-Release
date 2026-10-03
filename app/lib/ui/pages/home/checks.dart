part of '../home_page.dart';

/// «Проверка»: whether the VPN works and how fast. The speed test, the
/// traffic of the last days, the DNS leak test, and the way to the journal
/// for a message to support.
class ChecksPage extends StatelessWidget {
  final AppState state;
  const ChecksPage({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final leak = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Утечка DNS', sub: 'видит ли провайдер ваши запросы'),
          LeakCheck(state: state, first: true),
        ],
      ),
    );
    final journal = Panel(
      onTap: () => Nav.to(context, PageId.logs),
      child: Row(
        children: [
          Icon(Icons.receipt_long_outlined, color: p.muted),
          const SizedBox(width: 14),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('Журнал событий', style: display(16)),
                const SizedBox(height: 3),
                Text('Подключения, смены ядер и ошибки. Его можно скопировать и отправить в поддержку.', style: TextStyle(fontSize: 12, color: p.muted)),
              ],
            ),
          ),
          const SizedBox(width: 10),
          Icon(Icons.chevron_right, color: p.dim),
        ],
      ),
    );
    final left = [if (!state.speedUnsupported) _SpeedTestCard(state: state), if (!state.statsUnsupported) _TrafficCard(state: state)];
    final right = [leak, journal];
    List<Widget> spaced(List<Widget> ws) => [
      for (final (i, w) in ws.indexed) ...[if (i > 0) const SizedBox(height: 16), w],
    ];
    return PageFrame(
      children: [
        const PageHeader('Проверка', subtitle: 'Работает ли VPN, насколько он быстрый и не видит ли провайдер лишнего.'),
        LayoutBuilder(
          builder: (context, c) {
            if (c.maxWidth < 900) return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: spaced([...left, ...right]));
            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: spaced(left)),
                ),
                const SizedBox(width: 16),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: spaced(right)),
                ),
              ],
            );
          },
        ),
      ],
    );
  }
}
