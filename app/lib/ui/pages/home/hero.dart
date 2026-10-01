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
          if (st.settingsPending) ...[const SizedBox(height: 12), _PendingBanner(state: state)],
          _BackupBanner(state: state),
        ],
      ),
    );
  }
}
