part of '../home_page.dart';

/// «DNS напрямую» does not answer in this network (see
/// [AppStateDnsCheck.dnsFallbackHint]): the provider blocks it, as a white
/// list or a filter on DNS over HTTPS does, and the engine asks the
/// network's own resolvers for the sites that go direct. Everything works;
/// the user is told what went on and where to change it. Nothing changes
/// without a tap.
class _DnsFallbackBanner extends StatelessWidget {
  final AppState state;
  const _DnsFallbackBanner({required this.state});

  @override
  Widget build(BuildContext context) {
    final why = state.dnsFallbackWhy;
    return _Notice(
      color: warnColor,
      icon: Icons.dns_outlined,
      title: tr('home.dns.title'),
      text: tr('home.dns.text', {'address': state.dnsFallbackAddress, 'why': why.isEmpty ? '' : ' ($why)'}),
      actions: [
        Btn(
          label: tr('home.dns.settings'),
          small: true,
          kind: BtnKind.primary,
          onPressed: () {
            state.showDnsSettings();
            Nav.to(context, PageId.settings);
          },
        ),
        Btn(label: tr('home.btn.not_now'), small: true, onPressed: state.dismissDnsFallbackHint),
      ],
    );
  }
}
