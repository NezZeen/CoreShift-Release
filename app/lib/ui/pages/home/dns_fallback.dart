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
      title: 'DNS напрямую недоступен',
      text:
          'DNS напрямую (${state.dnsFallbackAddress}) недоступен в этой сети${why.isEmpty ? '' : ' ($why)'} — использую DNS сети. '
          'VPN работает, сайты напрямую открываются. Свой DNS с блокировкой рекламы лучше указать в поле «DNS через VPN».',
      actions: [
        Btn(
          label: 'Настройки DNS',
          small: true,
          kind: BtnKind.primary,
          onPressed: () {
            state.showDnsSettings();
            Nav.to(context, PageId.settings);
          },
        ),
        Btn(label: 'Не сейчас', small: true, onPressed: state.dismissDnsFallbackHint),
      ],
    );
  }
}
