part of '../home_page.dart';

/// The home page on a phone: the button, the way the traffic takes, while
/// connected the speed, and the speed test and traffic a line each.
class _CompactHome extends StatelessWidget {
  final AppState state;
  const _CompactHome({required this.state});

  @override
  Widget build(BuildContext context) {
    final st = state.status;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (state.subscriptionWarnings.isNotEmpty) ...[_SubWarningBanner(state: state), const SizedBox(height: 14)],
        _Hero(state: state, size: 150),
        const SizedBox(height: 20),
        if (state.vpnBlocked) ...[_VpnBlockedBanner(state: state), const SizedBox(height: 10)],
        if (st.state == ConnState.noNetwork) ...[_NoNetworkBanner(state: state), const SizedBox(height: 10)],
        if (state.serverUnresponsive) ...[_UnresponsiveBanner(state: state), const SizedBox(height: 10)],
        if (st.settingsPending) ...[_PendingBanner(state: state), const SizedBox(height: 10)],
        _BackupBanner(state: state, below: true),
        _Route(state: state),
        if (st.state == ConnState.connected) ...[const SizedBox(height: 10), _CompactSpeed(state: state)],
        const SizedBox(height: 10),
        _HomeTools(state: state),
      ],
    );
  }
}

/// While connected on a backup core: a line saying so, and the way back
/// without waiting for the timer. The rest about cores is on their page.
class _BackupBanner extends StatelessWidget {
  final AppState state;

  /// Spaced from what follows rather than from what precedes.
  final bool below;
  const _BackupBanner({required this.state, this.below = false});

  @override
  Widget build(BuildContext context) {
    final st = state.status;
    final chain = st.chain;
    final manual = state.setting('cores.mode', 'auto') == 'manual';
    if (st.state != ConnState.connected || manual || chain.length < 2 || st.core.isEmpty || st.core == chain.first) return const SizedBox();
    final primary = coreStyle(chain.first).name;
    final why = st.failed[chain.first] ?? '';
    return Padding(
      padding: below ? const EdgeInsets.only(bottom: 10) : const EdgeInsets.only(top: 12),
      child: Tooltip(
        message: why.isEmpty ? '' : '$primary: $why',
        child: _Notice(
          color: swapColor,
          icon: Icons.swap_horiz,
          text: '$primary не работает, подключено через ${coreStyle(st.core).name}',
          action: Btn(label: 'Вернуть $primary', small: true, loading: state.returning, onPressed: state.busy ? null : state.returnToPrimary),
        ),
      ),
    );
  }
}

/// The phone's speed while connected, down and up.
class _CompactSpeed extends StatelessWidget {
  final AppState state;
  const _CompactSpeed({required this.state});

  // Redrawn with every traffic sample, unlike the page around it.
  @override
  Widget build(BuildContext context) => ListenableBuilder(listenable: state.traffic, builder: (context, _) => _build(context));

  Widget _build(BuildContext context) {
    final p = context.pal;
    final (up, down) = state.speed.isEmpty ? (0, 0) : state.speed.last;
    Widget metric(IconData icon, Color color, String label, int rate) => Expanded(
      child: Row(
        children: [
          Icon(icon, size: 16, color: color),
          const SizedBox(width: 8),
          Flexible(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  label,
                  style: TextStyle(fontSize: 11, color: p.muted),
                  overflow: TextOverflow.ellipsis,
                ),
                Text(formatRate(rate), overflow: TextOverflow.ellipsis, style: figures(17)),
              ],
            ),
          ),
        ],
      ),
    );
    return Panel(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
      child: Row(children: [metric(Icons.south, okColor, 'Загрузка', down), metric(Icons.north, accent, 'Отдача', up)]),
    );
  }
}

class _PendingBanner extends StatelessWidget {
  final AppState state;
  const _PendingBanner({required this.state});

  @override
  Widget build(BuildContext context) => _Notice(
    color: warnColor,
    icon: Icons.info_outline,
    text: 'Настройки изменены и применятся после переподключения',
    action: Btn(label: 'Применить', small: true, onPressed: state.busy ? null : state.reconnect),
  );
}

/// The first screen before any subscription is added: what to do, in order.
class _Welcome extends StatelessWidget {
  final AppState state;
  const _Welcome({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    Widget step(int n, String title, String text, {bool last = false}) => IntrinsicHeight(
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // The steps as stations on a line, like the way the traffic takes.
          SizedBox(
            width: 30,
            child: Column(
              children: [
                Container(
                  width: 28,
                  height: 28,
                  alignment: Alignment.center,
                  decoration: BoxDecoration(
                    color: n == 1 ? accent : p.surface2,
                    shape: BoxShape.circle,
                    border: Border.all(color: n == 1 ? accent : p.border2, width: 2),
                  ),
                  child: Text(
                    '$n',
                    style: figures(14, weight: FontWeight.w600, color: n == 1 ? onAccent : p.muted),
                  ),
                ),
                if (!last) Expanded(child: Container(width: 2, color: p.border2)),
              ],
            ),
          ),
          const SizedBox(width: 14),
          Expanded(
            child: Padding(
              padding: EdgeInsets.only(top: 4, bottom: last ? 0 : 18),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(title, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 14.5)),
                  const SizedBox(height: 3),
                  Text(text, style: TextStyle(color: p.muted, fontSize: 13, height: 1.4)),
                ],
              ),
            ),
          ),
        ],
      ),
    );
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560),
        child: Padding(
          padding: const EdgeInsets.only(top: 24),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const CoreShiftMark(size: 44),
              const SizedBox(height: 18),
              Text('Добро пожаловать в CoreShift', style: display(isCompact(context) ? 26 : 32, spacing: -.3)),
              const SizedBox(height: 6),
              Text('Три шага до подключения:', style: TextStyle(color: p.muted, fontSize: 14)),
              const SizedBox(height: 24),
              step(1, 'Добавьте подписку', 'Скопируйте ссылку на подписку из бота, панели или письма провайдера и вставьте её сюда.'),
              step(2, 'Выберите сервер', 'CoreShift сам замерит пинг и подскажет самый быстрый.'),
              step(
                3,
                'Нажмите кнопку подключения',
                'Через VPN пойдут все приложения. Если одно ядро перестанет работать, CoreShift переключится на другое.',
                last: true,
              ),
              const SizedBox(height: 26),
              Btn(label: 'Добавить подписку', icon: Icons.add, kind: BtnKind.primary, onPressed: () => showAddSubscription(context, state)),
            ],
          ),
        ),
      ),
    );
  }
}
