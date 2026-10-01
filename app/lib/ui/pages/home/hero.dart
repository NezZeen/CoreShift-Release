part of '../home_page.dart';

class _Hero extends StatelessWidget {
  final AppState state;
  const _Hero({required this.state});

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

    return Panel(
      padding: const EdgeInsets.fromLTRB(22, 24, 22, 20),
      child: Column(
        children: [
          _ConnectButton(state: st.state, enabled: canConnect, onTap: state.toggleConnect),
          const SizedBox(height: 18),
          Text(
            title,
            style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: st.state == ConnState.idle ? p.text : color),
          ),
          const SizedBox(height: 4),
          SizedBox(
            height: 38,
            child: switch (st.state) {
              ConnState.connected when st.since != null => _Elapsed(since: st.since!, tun: st.tun),
              ConnState.failed => Tooltip(
                message: st.error,
                child: Text(
                  humanError(st.error),
                  textAlign: TextAlign.center,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(color: p.muted, fontSize: 12),
                ),
              ),
              ConnState.idle when sel.isEmpty => Text('Сначала выберите сервер', style: TextStyle(color: p.muted, fontSize: 13)),
              ConnState.idle when !sel.available => Text('Сервер «${sel.name}» пропал из подписки', style: const TextStyle(color: warnColor, fontSize: 13)),
              ConnState.idle => Text('Нажмите, чтобы подключиться', style: TextStyle(color: p.muted, fontSize: 13)),
              _ => const SizedBox(),
            },
          ),
          const SizedBox(height: 8),
          _NodePick(state: state),
          if (state.serverUnresponsive) ...[const SizedBox(height: 12), _UnresponsiveBanner(state: state)],
          if (st.settingsPending) ...[const SizedBox(height: 12), _PendingBanner(state: state)],
          _BackupBanner(state: state),
        ],
      ),
    );
  }
}

/// The connection is up but traffic does not get through: the server is
/// down, blocked or the network is bad. Says so, instead of "Подключено"
/// alone, and offers the one thing that can help from here.
class _UnresponsiveBanner extends StatelessWidget {
  final AppState state;
  const _UnresponsiveBanner({required this.state});

  @override
  Widget build(BuildContext context) => Container(
    padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
    decoration: BoxDecoration(
      color: errColor.withValues(alpha: .08),
      borderRadius: BorderRadius.circular(10),
      border: Border.all(color: errColor.withValues(alpha: .35)),
    ),
    child: Row(
      children: [
        const Icon(Icons.cloud_off_outlined, size: 16, color: errColor),
        const SizedBox(width: 9),
        const Expanded(
          child: Text(
            'Сервер не отвечает: связь через него не проходит. Он может быть недоступен или заблокирован. Попробуйте переподключиться или выберите другой сервер.',
            style: TextStyle(fontSize: 12),
          ),
        ),
        const SizedBox(width: 8),
        Btn(label: 'Переподключить', small: true, onPressed: state.busy ? null : state.reconnect),
      ],
    ),
  );
}
