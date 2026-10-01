part of '../home_page.dart';

/// The home page on a phone: the button, the server, and while connected
/// the speed. The chart stays on the desktop; the mode is in the settings.
class _CompactHome extends StatelessWidget {
  final AppState state;
  const _CompactHome({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final sel = state.selection;
    final (title, color) = switch (st.state) {
      ConnState.connected => ('Подключено', okColor),
      ConnState.connecting => ('Подключение…', accent),
      ConnState.disconnecting => ('Отключение…', p.muted),
      ConnState.failed => ('Ошибка подключения', errColor),
      ConnState.idle => ('Отключено', p.text),
    };
    final canConnect = state.online && (st.active || (sel.available && !state.busy));
    final muted = TextStyle(color: p.muted, fontSize: 13);
    final Widget line = switch (st.state) {
      ConnState.connected when st.since != null => _Elapsed(since: st.since!, tun: st.tun, short: true),
      ConnState.failed => Text(humanError(st.error), textAlign: TextAlign.center, maxLines: 3, overflow: TextOverflow.ellipsis, style: muted),
      ConnState.idle when sel.isEmpty => Text('Сначала выберите сервер', style: muted),
      ConnState.idle when !sel.available => Text(
        'Сервер «${sel.name}» пропал из подписки',
        textAlign: TextAlign.center,
        style: const TextStyle(color: warnColor, fontSize: 13),
      ),
      ConnState.idle => Text('Нажмите, чтобы подключиться', style: muted),
      _ => const SizedBox(),
    };
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const SizedBox(height: 16),
        Center(
          child: _ConnectButton(state: st.state, enabled: canConnect, onTap: state.toggleConnect, size: 150),
        ),
        const SizedBox(height: 26),
        Text(
          title,
          textAlign: TextAlign.center,
          style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: st.state == ConnState.idle ? p.text : color),
        ),
        const SizedBox(height: 4),
        ConstrainedBox(
          constraints: const BoxConstraints(minHeight: 20),
          child: Center(child: line),
        ),
        const SizedBox(height: 22),
        if (state.serverUnresponsive) ...[_UnresponsiveBanner(state: state), const SizedBox(height: 10)],
        if (st.settingsPending) ...[_PendingBanner(state: state), const SizedBox(height: 10)],
        _NodePick(state: state),
        _BackupBanner(state: state),
        if (st.state == ConnState.connected) ...[const SizedBox(height: 10), _CompactSpeed(state: state)],
      ],
    );
  }
}

/// While connected on a backup core: a line saying so, and the way back
/// without waiting for the timer. The rest about cores is on their page.
class _BackupBanner extends StatelessWidget {
  final AppState state;
  const _BackupBanner({required this.state});

  @override
  Widget build(BuildContext context) {
    final st = state.status;
    final chain = st.chain;
    final manual = state.setting('cores.mode', 'auto') == 'manual';
    if (st.state != ConnState.connected || manual || chain.length < 2 || st.core.isEmpty || st.core == chain.first) return const SizedBox();
    final primary = coreStyle(chain.first).name;
    final why = st.failed[chain.first] ?? '';
    return Padding(
      padding: const EdgeInsets.only(top: 12),
      child: Container(
        padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
        decoration: BoxDecoration(
          color: swapColor.withValues(alpha: .08),
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: swapColor.withValues(alpha: .35)),
        ),
        child: Row(
          children: [
            const Icon(Icons.swap_horiz, size: 16, color: swapColor),
            const SizedBox(width: 9),
            Expanded(
              child: Tooltip(
                message: why.isEmpty ? '' : '$primary: $why',
                child: Text('$primary не работает, подключено через ${coreStyle(st.core).name}', style: const TextStyle(fontSize: 12)),
              ),
            ),
            const SizedBox(width: 8),
            Btn(label: 'Вернуть $primary', small: true, loading: state.returning, onPressed: state.busy ? null : state.returnToPrimary),
          ],
        ),
      ),
    );
  }
}

/// The phone's speed while connected, down and up.
class _CompactSpeed extends StatelessWidget {
  final AppState state;
  const _CompactSpeed({required this.state});

  @override
  Widget build(BuildContext context) {
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
                Text(
                  formatRate(rate),
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, fontSize: 15, fontWeight: FontWeight.w500),
                ),
              ],
            ),
          ),
        ],
      ),
    );
    return Panel(
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      child: Row(children: [metric(Icons.south, okColor, 'Загрузка', down), metric(Icons.north, accent, 'Отдача', up)]),
    );
  }
}

class _PendingBanner extends StatelessWidget {
  final AppState state;
  const _PendingBanner({required this.state});

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
    decoration: BoxDecoration(
      color: warnColor.withValues(alpha: .08),
      borderRadius: BorderRadius.circular(10),
      border: Border.all(color: warnColor.withValues(alpha: .35)),
    ),
    child: Row(
      children: [
        const Icon(Icons.info_outline, size: 16, color: warnColor),
        const SizedBox(width: 9),
        const Expanded(child: Text('Настройки изменены и применятся после переподключения', style: TextStyle(fontSize: 12))),
        Btn(label: 'Применить', small: true, onPressed: state.busy ? null : state.reconnect),
      ],
    ),
  );
}

/// The first screen before any subscription is added: what to do, in order.
class _Welcome extends StatelessWidget {
  final AppState state;
  const _Welcome({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    Widget step(int n, String title, String text) => Padding(
      padding: const EdgeInsets.only(bottom: 14),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 26,
            height: 26,
            alignment: Alignment.center,
            decoration: BoxDecoration(color: accent.withValues(alpha: .15), shape: BoxShape.circle),
            child: Text(
              '$n',
              style: const TextStyle(color: accent, fontWeight: FontWeight.w700, fontSize: 13),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
                const SizedBox(height: 2),
                Text(text, style: TextStyle(color: p.muted, fontSize: 13)),
              ],
            ),
          ),
        ],
      ),
    );
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 560),
        child: Panel(
          padding: const EdgeInsets.fromLTRB(28, 30, 28, 26),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('Добро пожаловать в CoreShift', style: TextStyle(fontSize: 22, fontWeight: FontWeight.w600)),
              const SizedBox(height: 6),
              Text('Три шага до подключения:', style: TextStyle(color: p.muted)),
              const SizedBox(height: 22),
              step(1, 'Добавьте подписку', 'Скопируйте ссылку на подписку из бота, панели или письма провайдера и вставьте её сюда.'),
              step(2, 'Выберите сервер', 'CoreShift сам замерит пинг и подскажет самый быстрый.'),
              step(3, 'Нажмите кнопку подключения', 'Через VPN пойдут все приложения. Если одно ядро перестанет работать, CoreShift переключится на другое.'),
              const SizedBox(height: 8),
              Btn(label: 'Добавить подписку', icon: Icons.add, kind: BtnKind.primary, onPressed: () => showAddSubscription(context, state)),
            ],
          ),
        ),
      ),
    );
  }
}
