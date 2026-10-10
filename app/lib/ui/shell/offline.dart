part of '../shell.dart';

class _Offline extends StatefulWidget {
  final AppState state;
  const _Offline({required this.state});

  @override
  State<_Offline> createState() => _OfflineState();
}

class _OfflineState extends State<_Offline> {
  bool starting = false;
  String? error;

  Future<void> _start() async {
    setState(() {
      starting = true;
      error = null;
    });
    final err = await platform.startService();
    if (!mounted) return;
    // On success the window connects by itself once the service is up.
    setState(() {
      starting = false;
      error = err;
    });
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final state = widget.state;
    // Not in the group the service's token is for: waiting will not help,
    // an administrator will (the reason says how).
    final denied = platform.daemonAccessDenied(state.offlineReason);
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 520),
        child: Panel(
          padding: const EdgeInsets.all(26),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  if (denied)
                    const Icon(Icons.lock_outline, size: 20, color: errColor)
                  else
                    SizedBox(width: 18, height: 18, child: CircularProgressIndicator(strokeWidth: 2, color: p.muted)),
                  const SizedBox(width: 12),
                  Flexible(
                    child: Text(
                      denied
                          ? 'Нет доступа к службе'
                          : platform.isAndroid
                          ? 'Запуск CoreShift…'
                          : state.daemonStarting
                          ? 'Запуск службы…'
                          : 'Подключение к службе…',
                      style: dialogTitle,
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 12),
              // Selectable: it holds the command to give the administrator.
              SelectableText(
                state.offlineReason.isEmpty ? (platform.isAndroid ? 'Запускаем движок' : 'Ищем службу CoreShift') : state.offlineReason,
                style: TextStyle(color: p.muted),
              ),
              const SizedBox(height: 14),
              Text(
                denied
                    ? 'Служба CoreShift управляет VPN всего компьютера, поэтому ею пользуются только администраторы и участники группы '
                          '«${platform.isLinux ? 'coreshift' : windowsGroup}». Установщик добавляет в неё того, кто установил CoreShift.'
                    : platform.isAndroid
                    ? 'Движок VPN работает внутри приложения и обычно запускается за секунду. '
                          'Если этот экран не пропадает, закройте CoreShift в списке недавних приложений и откройте снова.'
                    : platform.isLinux
                    ? 'VPN работает через системную службу CoreShift. Она запускается вместе с компьютером и держит VPN, '
                          'пока его не выключат кнопкой, даже если окно закрыто.'
                    : state.daemonStartRefused
                    ? 'VPN работает через фоновую службу CoreShift. Windows не дал запустить её без прав администратора: '
                          'так бывает со службой, установленной версией до 0.4. Запустите её кнопкой ниже или переустановите CoreShift.'
                    : 'VPN работает через фоновую службу CoreShift. Она запускается вместе с приложением '
                          'и останавливается, когда его закрывают. Обычно это занимает пару секунд.',
                style: TextStyle(color: p.muted, fontSize: 13, height: 1.5),
              ),
              // Without access to the service, starting it would not help.
              if (platform.canStartService && !state.daemonStarting && !platform.daemonAccessDenied(state.offlineReason)) ...[
                const SizedBox(height: 16),
                Row(
                  children: [
                    Btn(label: 'Запустить службу', icon: Icons.play_arrow, kind: BtnKind.primary, loading: starting, onPressed: _start),
                    const SizedBox(width: 10),
                    Flexible(
                      child: Text(
                        platform.isLinux ? 'Система спросит пароль администратора' : 'Windows попросит права администратора',
                        style: TextStyle(color: p.dim, fontSize: 12),
                      ),
                    ),
                  ],
                ),
                if (error != null) ...[const SizedBox(height: 8), Text(error!, style: const TextStyle(color: errColor, fontSize: 12))],
              ],
              if (!platform.isAndroid) ...[
                const SizedBox(height: 16),
                Text(
                  'Адрес службы берётся из ${state.backend.description}',
                  style: TextStyle(color: p.dim, fontSize: 11, fontFamily: monoFont, fontFamilyFallback: monoFallback),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _OfflineBanner extends StatefulWidget {
  final String reason;
  const _OfflineBanner({required this.reason});

  @override
  State<_OfflineBanner> createState() => _OfflineBannerState();
}

class _OfflineBannerState extends State<_OfflineBanner> {
  bool starting = false;
  String? error;

  /// Linux: the service stopped while the window was open. Nothing starts
  /// it again by itself (on Windows the app does), so offer what the
  /// start screen offers.
  bool get _canStart => platform.isLinux && platform.canStartService && !platform.daemonAccessDenied(widget.reason);

  Future<void> _start() async {
    setState(() {
      starting = true;
      error = null;
    });
    final err = await platform.startService();
    if (!mounted) return;
    setState(() {
      starting = false;
      error = err;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 9),
      color: errColor.withValues(alpha: .12),
      child: Row(
        children: [
          const Icon(Icons.link_off, size: 16, color: errColor),
          const SizedBox(width: 10),
          Expanded(child: Text(error == null ? '${widget.reason}. Переподключаемся…' : '${widget.reason}. $error', style: const TextStyle(fontSize: 13))),
          if (_canStart) ...[
            const SizedBox(width: 10),
            Btn(label: 'Запустить службу', icon: Icons.play_arrow, small: true, loading: starting, onPressed: _start),
          ],
        ],
      ),
    );
  }
}
