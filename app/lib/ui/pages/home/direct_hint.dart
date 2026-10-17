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
      title: tr('home.direct.title'),
      text: tr('home.direct.text', {'fix': tr(fixable ? 'home.direct.fix_all' : 'home.direct.fix_lists')}),
      actions: [
        if (fixable)
          Btn(label: tr('home.btn.all_vpn'), small: true, kind: BtnKind.primary, onPressed: state.busy ? null : state.fixDirect)
        else
          Btn(label: tr('home.btn.rules'), small: true, kind: BtnKind.primary, onPressed: () => Nav.to(context, PageId.routing)),
        Btn(label: tr('home.btn.not_now'), small: true, onPressed: state.dismissDirectHint),
      ],
    );
  }
}
