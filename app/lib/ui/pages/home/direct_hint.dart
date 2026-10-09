part of '../home_page.dart';

/// Direct connections do not get through this network while the VPN works
/// (see [AppStateDirectHint]): sites the settings send direct stay blank.
/// Offers to send everything through the VPN, which reconnects; or, when
/// only the user's own direct lists send traffic around it, the rules page.
/// «Не сейчас» puts it off; nothing changes without a tap.
class _DirectHintBanner extends StatelessWidget {
  final AppState state;
  const _DirectHintBanner({required this.state});

  @override
  Widget build(BuildContext context) {
    final fixable = state.directFixable;
    return _Notice(
      color: warnColor,
      icon: Icons.alt_route,
      title: 'Сайты напрямую не открываются',
      text:
          'В этой сети прямые соединения не проходят, а через VPN всё работает: похоже, оператор пропускает только белый список '
          'или российские сайты отсюда недоступны. '
          '${fixable ? 'Пустите весь трафик через VPN — CoreShift переподключится.' : 'Уберите из своих списков «напрямую» то, что здесь не открывается.'}',
      actions: [
        if (fixable)
          Btn(label: 'Всё через VPN', small: true, kind: BtnKind.primary, onPressed: state.busy ? null : state.fixDirect)
        else
          Btn(label: 'Правила', small: true, kind: BtnKind.primary, onPressed: () => Nav.to(context, PageId.routing)),
        Btn(label: 'Не сейчас', small: true, onPressed: state.dismissDirectHint),
      ],
    );
  }
}
