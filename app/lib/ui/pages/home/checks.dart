part of '../home_page.dart';

/// «Проверка»: whether the VPN works and how fast. The speed test, the DNS
/// leak test and the traffic of the last days.
class ChecksPage extends StatelessWidget {
  final AppState state;
  const ChecksPage({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    final leak = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Утечка DNS', sub: 'видит ли провайдер ваши запросы'),
          LeakCheck(state: state, first: true),
        ],
      ),
    );
    final left = [if (!state.speedUnsupported) _SpeedTestCard(state: state), leak];
    final right = [if (!state.statsUnsupported) _TrafficCard(state: state)];
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
