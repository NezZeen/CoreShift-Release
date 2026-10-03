import 'package:flutter/material.dart';

import '../../api/models.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/leak.dart';
import '../countries.dart';
import '../theme.dart';
import '../widgets.dart';

/// The DNS leak test: a button and what it found.
class LeakCheck extends StatelessWidget {
  final AppState state;
  final bool first;
  const LeakCheck({super.key, required this.state, this.first = false});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final ready = st.state == ConnState.connected && st.tun;
    final report = state.leakReport;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SettingRow(
          first: first,
          title: 'Проверка утечки DNS',
          description: ready
              ? 'Узнать, видит ли провайдер ваши DNS-запросы. Проверка идёт через туннель, как у приложений. Использует сервис bash.ws'
              : 'Подключитесь в режиме «Все приложения», чтобы проверить',
          trailing: Btn(label: 'Проверить DNS', icon: Icons.policy_outlined, loading: state.leakTesting, onPressed: ready ? state.runLeakTest : null),
        ),
        if (state.leakError.isNotEmpty)
          _Box(
            color: errColor,
            icon: Icons.error_outline,
            title: 'Проверка не удалась',
            children: [Text(state.leakError, style: TextStyle(fontSize: 12, color: p.muted))],
          )
        else if (report != null)
          LeakResultCard(report: report),
      ],
    );
  }
}

/// What a leak test found: the verdict, what to do, and the addresses
/// behind it.
class LeakResultCard extends StatelessWidget {
  final LeakReport report;
  const LeakResultCard({super.key, required this.report});

  static String get _advice => platform.isAndroid
      ? 'Что сделать: уберите «Частный DNS», заданный вручную в настройках Android, включите «Защиту от утечек DNS» выше, '
            'переподключитесь и проверьте снова.'
      : 'Что сделать: включите «Защиту от утечек DNS» выше, переподключитесь и проверьте снова. '
            'Если не поможет, проверьте, не идёт ли bash.ws напрямую по вашим правилам.';

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final verdict = report.verdict;
    final suspicious = report.suspicious;
    final small = TextStyle(fontSize: 12, color: p.muted);
    final heading = small.copyWith(fontWeight: FontWeight.w600);
    Widget row(IconData icon, Color color, List<InlineSpan> spans) => Padding(
      padding: const EdgeInsets.only(top: 3),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(icon, size: 14, color: color),
          const SizedBox(width: 6),
          Expanded(
            child: Text.rich(TextSpan(children: spans), style: small),
          ),
        ],
      ),
    );
    TextSpan mono(String s) => TextSpan(
      text: s,
      style: TextStyle(fontFamily: monoFont, fontFamilyFallback: monoFallback, color: p.text),
    );
    Widget server(LeakServer s, {bool bad = false}) => row(bad ? Icons.warning_amber_rounded : Icons.dns_outlined, bad ? warnColor : p.dim, [
      mono(s.ip),
      if (s.place.isNotEmpty) TextSpan(text: '  ${s.place}'),
    ]);
    List<Widget> servers(String title, List<LeakServer> list) => [
      const SizedBox(height: 8),
      Text(title, style: heading),
      for (final d in list) server(d, bad: suspicious.contains(d)),
    ];

    final (color, icon, title, text) = switch (verdict) {
      LeakVerdict.ok => (
        okColor,
        Icons.verified_user_outlined,
        'Утечки нет',
        report.fakeIp
            ? 'Адреса сайтов приложениям выдаёт сам туннель, а имена разрешает VPN-сервер. Провайдер ваших DNS-запросов не видит.'
            : 'DNS-запросы приложений уходят через VPN к серверам на его стороне. Провайдер их не видит.',
      ),
      LeakVerdict.leak => (
        errColor,
        Icons.warning_amber_rounded,
        'Есть утечка DNS',
        report.ispResolvers
            ? 'Часть DNS-запросов обрабатывает DNS вашего провайдера: он видит, какие сайты вы открываете.'
            : 'Часть DNS-запросов обрабатывают DNS-серверы не из страны VPN-сервера, возможно, провайдера.',
      ),
      LeakVerdict.bypass => (
        warnColor,
        Icons.alt_route,
        'Проверка прошла мимо VPN',
        'bash.ws увидел ваш настоящий адрес, поэтому результат недостоверен. Похоже, сервер пускает этот сайт напрямую. '
            'Выберите другой сервер или переподключитесь и проверьте снова.',
      ),
      LeakVerdict.unknown => (
        warnColor,
        Icons.help_outline,
        'Результат неточный',
        report.exit == null
            ? 'bash.ws не сообщил, откуда пришла проверка. Попробуйте ещё раз.'
            : 'Не удалось узнать ваш адрес без VPN, поэтому нельзя сказать, шла ли проверка через VPN. Попробуйте ещё раз.',
      ),
    };

    final system = [
      for (final d in report.dns)
        if (d.path != 'proxy') d,
    ];
    final proxy = [
      for (final d in report.dns)
        if (d.path == 'proxy') d,
    ];
    final home = report.home;
    return _Box(
      color: color,
      icon: icon,
      title: title,
      children: [
        Text(text, style: small),
        if (verdict == LeakVerdict.leak) ...[const SizedBox(height: 6), Text(_advice, style: small.copyWith(color: p.text))],
        if (report.exit != null) ...[const SizedBox(height: 8), Text('Сайты видят вас из', style: heading), server(report.exit!, bad: report.bypassed)],
        if (verdict == LeakVerdict.bypass && home != null) ...[
          const SizedBox(height: 8),
          Text('Ваш адрес без VPN', style: heading),
          row(Icons.home_outlined, p.dim, [mono(home.ip), if (home.country.isNotEmpty) TextSpan(text: '  ${countryName(home.country)}')]),
        ],
        // Around the VPN the two paths mean nothing apart.
        if (verdict == LeakVerdict.bypass) ...[
          if (report.dns.isNotEmpty) ...servers('DNS-серверы', report.dns),
        ] else ...[
          if (system.isNotEmpty)
            ...servers('DNS приложений', system)
          else if (report.systemChecked && report.fakeIp) ...[
            const SizedBox(height: 8),
            Text('DNS приложений', style: heading),
            row(Icons.shield_outlined, p.dim, [const TextSpan(text: 'Отвечает сам туннель, наружу запросы не уходят')]),
          ],
          if (proxy.isNotEmpty) ...servers('DNS на стороне VPN-сервера', proxy),
        ],
        if (!report.systemChecked) ...[const SizedBox(height: 8), Text('Запросы приложений через туннель проверить не удалось.', style: small)],
      ],
    );
  }
}

class _Box extends StatelessWidget {
  final Color color;
  final IconData icon;
  final String title;
  final List<Widget> children;
  const _Box({required this.color, required this.icon, required this.title, required this.children});

  @override
  Widget build(BuildContext context) => Container(
    margin: const EdgeInsets.only(top: 4, bottom: 8),
    padding: const EdgeInsets.all(12),
    decoration: BoxDecoration(
      color: color.withValues(alpha: .07),
      borderRadius: BorderRadius.circular(10),
      border: Border.all(color: color.withValues(alpha: .35)),
    ),
    child: Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 18, color: color),
        const SizedBox(width: 10),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
              const SizedBox(height: 4),
              ...children,
            ],
          ),
        ),
      ],
    ),
  );
}
