import 'package:flutter/material.dart';

import '../../api/models.dart';
import '../../l10n/strings.dart';
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
          title: tr('leak.title'),
          description: ready ? tr('leak.desc.ready') : tr('leak.desc.not_ready'),
          trailing: Btn(label: tr('leak.btn'), icon: Icons.policy_outlined, loading: state.leakTesting, onPressed: ready ? state.runLeakTest : null),
        ),
        if (state.leakError.isNotEmpty)
          _Box(
            color: errColor,
            icon: Icons.error_outline,
            title: tr('leak.failed'),
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

  static String get _advice => tr(platform.isAndroid ? 'leak.advice.android' : 'leak.advice.other');

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final verdict = report.verdict;
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
    // A tick for the user's own DNS and the VPN's side, a warning only for
    // what may really leak; what is not known stays neutral.
    Widget server(LeakServer s, {bool bad = false, bool good = false}) => row(
      bad
          ? Icons.warning_amber_rounded
          : good
          ? Icons.check_circle_outline
          : Icons.dns_outlined,
      bad
          ? warnColor
          : good
          ? okColor
          : p.dim,
      [mono(s.ip), if (s.place.isNotEmpty) TextSpan(text: '  ${s.place}'), if (s.role == 'own') TextSpan(text: '  · ${tr('leak.own')}')],
    );
    List<Widget> servers(String title, List<LeakServer> list) => [
      const SizedBox(height: 8),
      Text(title, style: heading),
      for (final d in list) server(d, bad: d.role == 'isp', good: d.role == 'own' || d.role == 'vpn'),
    ];
    // «AdGuard, Германия»: the provider and where the answering servers are.
    final ownPlace = [
      if (report.provider.isNotEmpty) report.provider,
      ...{
        for (final d in report.ownDns)
          if (d.country.isNotEmpty) countryName(d.country),
      },
    ].join(', ');
    final appsTitle = tr('leak.dns.apps', {'provider': report.provider.isEmpty ? '' : ' — ${report.provider}'});

    final (color, icon, title, text) = switch (verdict) {
      LeakVerdict.ok => (
        okColor,
        Icons.verified_user_outlined,
        tr('leak.ok.title'),
        report.ownDns.isNotEmpty
            ? tr('leak.ok.own', {'place': ownPlace.isEmpty ? '' : ' ($ownPlace)'})
            : report.fakeIp
            ? tr('leak.ok.fakeip')
            : tr('leak.ok.vpn'),
      ),
      LeakVerdict.leak => (errColor, Icons.warning_amber_rounded, tr('leak.leak.title'), tr('leak.leak.text')),
      LeakVerdict.bypass => (warnColor, Icons.alt_route, tr('leak.bypass.title'), tr('leak.bypass.text')),
      LeakVerdict.unknown => (
        warnColor,
        Icons.help_outline,
        tr('leak.unknown.title'),
        report.exit == null ? tr('leak.unknown.no_exit') : tr('leak.unknown.no_home'),
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
    final geo = report.exitGeo;
    final differ = !report.bypassed && report.exitCountriesDiffer;
    return _Box(
      color: color,
      icon: icon,
      title: title,
      children: [
        Text(text, style: small),
        if (verdict == LeakVerdict.leak) ...[const SizedBox(height: 6), Text(_advice, style: small.copyWith(color: p.text))],
        if (report.exit != null) ...[
          const SizedBox(height: 8),
          Text(tr(report.bypassed ? 'leak.exit.seen' : 'leak.exit.vpn'), style: heading),
          if (differ) ...[
            // The services disagree: the country most name, and how they voted.
            row(Icons.dns_outlined, p.dim, [mono(report.exit!.ip), TextSpan(text: '  ${_voteText(report.exitGeo!)}')]),
            if (report.exitGeo!.split)
              Padding(
                padding: const EdgeInsets.only(left: 20, top: 3),
                child: Text(_splitText(report.exitGeo!), style: small),
              ),
          ] else
            server(report.exit!, bad: report.bypassed),
          if (geo != null && !report.bypassed) _GeoDetails(geo: geo),
        ],
        if (verdict == LeakVerdict.bypass && home != null) ...[
          const SizedBox(height: 8),
          Text(tr('leak.home'), style: heading),
          row(Icons.home_outlined, p.dim, [mono(home.ip), if (home.country.isNotEmpty) TextSpan(text: '  ${countryName(home.country)}')]),
        ],
        // Around the VPN the two paths mean nothing apart.
        if (verdict == LeakVerdict.bypass) ...[
          if (report.dns.isNotEmpty) ...servers(tr('leak.dns.servers'), report.dns),
        ] else ...[
          if (system.isNotEmpty)
            ...servers(appsTitle, system)
          else if (report.systemChecked && report.fakeIp) ...[
            const SizedBox(height: 8),
            Text(appsTitle, style: heading),
            row(Icons.shield_outlined, p.dim, [TextSpan(text: tr('leak.tunnel_answers'))]),
          ],
          if (proxy.isNotEmpty) ...servers(tr('leak.dns.vpn_side'), proxy),
        ],
        if (report.dns.isNotEmpty || differ)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(tr('leak.country_note'), style: small),
          ),
        if (!report.systemChecked) ...[const SizedBox(height: 8), Text(tr('leak.apps_failed'), style: small)],
      ],
    );
  }
}

/// «Россия, Москва — 4 из 5 баз» (and «поровну» when the votes are even).
String _voteText(LeakGeo g) {
  final place = [countryName(g.country), cityName(g.city)].where((s) => s.isNotEmpty).join(', ');
  return tr(g.tie ? 'leak.vote.tie' : 'leak.vote', {'place': place, 'votes': g.votes, 'total': g.total});
}

/// «Россия (ipwho.is, ipinfo.io) · Украина (bash.ws)»: who said what.
String _splitText(LeakGeo g) => g.groups.map((x) => '${countryName(x.country)} (${x.sources.join(', ')})').join(' · ');

/// Each geolocation service and what it said of the VPN server's address,
/// folded away: the ones that did not answer are only named here.
class _GeoDetails extends StatefulWidget {
  final LeakGeo geo;
  const _GeoDetails({required this.geo});

  @override
  State<_GeoDetails> createState() => _GeoDetailsState();
}

class _GeoDetailsState extends State<_GeoDetails> {
  bool _open = false;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final small = TextStyle(fontSize: 12, color: p.muted);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SizedBox(height: 4),
        InkWell(
          onTap: () => setState(() => _open = !_open),
          borderRadius: BorderRadius.circular(6),
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 4),
            child: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(_open ? Icons.expand_less : Icons.expand_more, size: 16, color: p.dim),
                const SizedBox(width: 4),
                Text(tr('leak.geo.more'), style: small.copyWith(color: p.text)),
              ],
            ),
          ),
        ),
        if (_open)
          for (final s in widget.geo.sources)
            Padding(
              padding: const EdgeInsets.only(left: 20, top: 2),
              child: Text.rich(
                TextSpan(
                  children: [
                    TextSpan(
                      text: s.name,
                      style: TextStyle(color: p.text, fontWeight: FontWeight.w600),
                    ),
                    TextSpan(
                      text: s.ok
                          ? ' — ${[countryName(s.country), cityName(s.city), s.org].where((x) => x.isNotEmpty).join(', ')}'
                          : ' — ${tr('leak.geo.no_answer')}',
                    ),
                  ],
                ),
                style: small,
              ),
            ),
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
