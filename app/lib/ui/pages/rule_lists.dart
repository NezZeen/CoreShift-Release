import 'package:flutter/material.dart';

import '../../api/punycode.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../theme.dart';
import '../widgets.dart';

/// An address or subnet ("203.0.113.7", "10.8.0.0/16", "2001:db8::/32")
/// rather than a site name.
bool isIpEntry(String v) => RegExp(r'^[0-9.]+(/\d{1,2})?$').hasMatch(v) || (v.contains(':') && RegExp(r'^[0-9a-fA-F:.]+(/\d{1,3})?$').hasMatch(v));

/// Splits what the user typed or pasted into separate entries.
List<String> splitEntries(String input) => input.split(RegExp(r'[\s,;]+')).map((d) => d.trim()).where((d) => d.isNotEmpty).toList();

/// Sites and addresses in one list: entries that look like addresses go to
/// [ipsKey], the rest to [domainsKey]. The daemon checks and tidies both.
class RuleListPanel extends StatefulWidget {
  final AppState state;
  final String title;
  final String? sub;
  final String? description;
  final String domainsKey;
  final String? ipsKey;
  final String hint;
  final String empty;
  final String removeTip;
  final Color? chipColor;

  /// Without its own panel and title: inside a [Fold] that has them.
  final bool bare;

  const RuleListPanel({
    this.bare = false,
    super.key,
    required this.state,
    required this.title,
    this.sub,
    this.description,
    required this.domainsKey,
    this.ipsKey,
    required this.hint,
    required this.empty,
    required this.removeTip,
    this.chipColor,
  });

  @override
  State<RuleListPanel> createState() => _RuleListPanelState();
}

class _RuleListPanelState extends State<RuleListPanel> {
  final _add = TextEditingController();
  String? _error;

  AppState get s => widget.state;

  /// An older daemon does not know the address lists and would reject them.
  String? get _ipsKey => widget.ipsKey != null && s.hasSetting(widget.ipsKey!) ? widget.ipsKey : null;

  List<String> _list(String? key) => key == null ? const [] : s.setting<List>(key, const []).cast<String>();

  Future<bool> _save(List<String> domains, List<String> ips) async {
    final ipsKey = _ipsKey;
    final err = await s.updateSettings((x) {
      x['routing'][widget.domainsKey.split('.').last] = domains;
      if (ipsKey != null) x['routing'][ipsKey.split('.').last] = ips;
    });
    if (mounted) setState(() => _error = err);
    return err == null;
  }

  Future<void> _addEntries() async {
    final input = splitEntries(_add.text);
    if (input.isEmpty) return;
    final ips = _ipsKey == null ? const <String>[] : input.where(isIpEntry).toList();
    final names = input.where((e) => !ips.contains(e)).toList();
    final domains = _list(widget.domainsKey), addrs = _list(_ipsKey);
    final before = domains.length + addrs.length;
    if (await _save({...domains, ...names}.toList(), {...addrs, ...ips}.toList())) {
      _add.clear();
      final added = _list(widget.domainsKey).length + _list(_ipsKey).length - before;
      if (added > 0) s.toast('Добавлено: $added', ToastKind.ok);
    }
  }

  @override
  void dispose() {
    _add.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final domains = _list(widget.domainsKey), ips = _list(_ipsKey);
    final body = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (!widget.bare) PanelTitle(widget.title, sub: widget.sub, info: widget.description),
        if (widget.description != null && (!isCompact(context) || widget.bare)) ...[
          Text(widget.description!, style: TextStyle(fontSize: 12, color: p.muted, height: 1.45)),
          const SizedBox(height: 12),
        ],
        Row(
          children: [
            Expanded(
              child: TextField(
                controller: _add,
                onSubmitted: (_) => _addEntries(),
                onChanged: (_) => setState(() => _error = null),
                style: const TextStyle(fontSize: 13, fontFamily: monoFont, fontFamilyFallback: monoFallback),
                decoration: InputDecoration(hintText: widget.hint),
              ),
            ),
            const SizedBox(width: 8),
            Btn(label: 'Добавить', icon: Icons.add, onPressed: _addEntries),
          ],
        ),
        if (_error != null)
          Padding(
            padding: const EdgeInsets.only(top: 8),
            child: Text(_error!, style: const TextStyle(color: errColor, fontSize: 12)),
          ),
        const SizedBox(height: 12),
        if (domains.isEmpty && ips.isEmpty)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 14),
            child: Center(
              child: Text(widget.empty, style: TextStyle(color: p.dim, fontSize: 12)),
            ),
          )
        else
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: [
              // Names in other scripts are kept in punycode, shown as typed.
              for (final d in domains) _chip(context, domainToUnicode(d), () => _save(domains.where((x) => x != d).toList(), ips)),
              for (final a in ips) _chip(context, a, () => _save(domains, ips.where((x) => x != a).toList()), ip: true),
            ],
          ),
      ],
    );
    return widget.bare ? body : Panel(child: body);
  }

  Widget _chip(BuildContext context, String text, VoidCallback onRemove, {bool ip = false}) {
    final p = context.pal;
    final c = widget.chipColor;
    return Container(
      padding: EdgeInsets.fromLTRB(ip ? 8 : 10, 4, 4, 4),
      decoration: BoxDecoration(
        color: c == null ? p.surface2 : c.withValues(alpha: .08),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: c == null ? p.border : c.withValues(alpha: .35)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (ip) ...[Icon(Icons.lan_outlined, size: 13, color: p.muted), const SizedBox(width: 5)],
          Text(
            text,
            style: const TextStyle(fontSize: 13, fontFamily: monoFont, fontFamilyFallback: monoFallback),
          ),
          const SizedBox(width: 2),
          Tooltip(
            message: widget.removeTip,
            child: InkWell(
              borderRadius: BorderRadius.circular(6),
              onTap: onRemove,
              child: Padding(
                padding: const EdgeInsets.all(4),
                child: Icon(Icons.close, size: 14, color: p.dim),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// A popular service that is blocked or unavailable in Russia, as the lists
/// it needs to go through the VPN.
class ServicePreset {
  final String name;
  final IconData icon;
  final List<String> domains;
  final List<String> ips;
  final List<String> apps;
  const ServicePreset(this.name, this.icon, {required this.domains, this.ips = const [], this.apps = const []});

  /// [apps] as this system names its programs: on Linux without ".exe"
  /// (Discord runs as …/Discord, and is matched by its path); Android picks
  /// the apps in the VPN in a list of its own.
  List<String> get localApps => presetApps(apps, linux: platform.isLinux, android: platform.isAndroid);
}

/// Programs named as on Windows ("Discord.exe"), as [linux] or [android]
/// names them; see [ServicePreset.localApps].
List<String> presetApps(List<String> apps, {required bool linux, required bool android}) => android
    ? const []
    : linux
    ? [for (final a in apps) a.replaceFirst(RegExp(r'\.exe$', caseSensitive: false), '')]
    : apps;

const servicePresets = [
  ServicePreset(
    'YouTube',
    Icons.smart_display_outlined,
    domains: [
      'youtube.com',
      'youtu.be',
      'googlevideo.com',
      'ytimg.com',
      'ggpht.com',
      'youtube-nocookie.com',
      'youtubei.googleapis.com',
      'yt3.googleusercontent.com',
    ],
  ),
  ServicePreset(
    'Instagram и Facebook',
    Icons.photo_camera_outlined,
    domains: ['instagram.com', 'cdninstagram.com', 'facebook.com', 'fbcdn.net', 'fb.com', 'fbsbx.com', 'facebook.net'],
  ),
  // Telegram connects to its servers by address, so the addresses matter
  // more than the names. From core.telegram.org/resources/cidr.txt.
  ServicePreset(
    'Telegram',
    Icons.send_outlined,
    domains: ['telegram.org', 't.me', 'telegram.me', 'telesco.pe', 'telegra.ph', 'tdesktop.com'],
    ips: [
      '91.108.4.0/22',
      '91.108.8.0/22',
      '91.108.12.0/22',
      '91.108.16.0/22',
      '91.108.20.0/22',
      '91.108.56.0/22',
      '91.105.192.0/23',
      '149.154.160.0/20',
      '185.76.151.0/24',
      '2001:b28:f23c::/48',
      '2001:b28:f23d::/48',
      '2001:b28:f23f::/48',
      '2001:67c:4e8::/48',
      '2a0a:f280::/32',
    ],
  ),
  // Voice chats go to servers by address, so the app itself is listed too.
  ServicePreset(
    'Discord',
    Icons.headset_mic_outlined,
    domains: ['discord.com', 'discord.gg', 'discordapp.com', 'discordapp.net', 'discord.media', 'discordcdn.com', 'discord.dev'],
    apps: ['Discord.exe'],
  ),
  ServicePreset('WhatsApp', Icons.chat_outlined, domains: ['whatsapp.com', 'whatsapp.net', 'wa.me']),
  ServicePreset('X (Twitter)', Icons.alternate_email, domains: ['x.com', 'twitter.com', 'twimg.com', 't.co']),
  ServicePreset('ChatGPT', Icons.auto_awesome_outlined, domains: ['openai.com', 'chatgpt.com', 'oaistatic.com', 'oaiusercontent.com']),
  ServicePreset('Claude', Icons.auto_awesome_outlined, domains: ['claude.ai', 'claude.com', 'anthropic.com']),
  ServicePreset('Spotify', Icons.music_note_outlined, domains: ['spotify.com', 'scdn.co', 'spotifycdn.com', 'spotify.design']),
];

/// One click puts a whole service through the VPN in the "only selected"
/// mode, or takes it out again.
class ServicePresetsPanel extends StatelessWidget {
  final AppState state;
  const ServicePresetsPanel({super.key, required this.state});

  List<String> _list(String key) => state.setting<List>('routing.$key', const []).cast<String>();

  bool _on(ServicePreset sp) {
    final domains = _list('proxy_domains').toSet(), ips = _list('proxy_ips').toSet();
    final apps = _list('proxy_apps').map((a) => a.toLowerCase()).toSet();
    return domains.containsAll(sp.domains) && ips.containsAll(sp.ips) && apps.containsAll(sp.localApps.map((a) => a.toLowerCase()));
  }

  Future<void> _toggle(ServicePreset sp) async {
    final on = _on(sp);
    final err = await state.updateSettings((x) {
      final r = x['routing'] as Map;
      List<String> edit(String key, List<String> preset, {bool fold = false}) {
        final cur = (r[key] as List? ?? const []).cast<String>();
        bool inPreset(String v) => preset.any((p) => fold ? p.toLowerCase() == v.toLowerCase() : p == v);
        return on
            ? cur.where((v) => !inPreset(v)).toList()
            : [...cur, ...preset.where((p) => !cur.any((v) => fold ? v.toLowerCase() == p.toLowerCase() : v == p))];
      }

      r['proxy_domains'] = edit('proxy_domains', sp.domains);
      r['proxy_ips'] = edit('proxy_ips', sp.ips);
      r['proxy_apps'] = edit('proxy_apps', sp.localApps, fold: true);
    });
    if (err == null) state.toast(on ? '${sp.name} больше не идёт через VPN' : '${sp.name} теперь идёт через VPN', ToastKind.ok);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Популярные сервисы', sub: 'в один клик'),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final sp in servicePresets)
                Builder(
                  builder: (context) {
                    final on = _on(sp);
                    return Tooltip(
                      message: [...sp.domains, if (sp.ips.isNotEmpty) '+ адреса серверов', ...sp.localApps].join(', '),
                      waitDuration: const Duration(milliseconds: 600),
                      child: InkWell(
                        borderRadius: BorderRadius.circular(10),
                        onTap: () => _toggle(sp),
                        child: AnimatedContainer(
                          duration: const Duration(milliseconds: 150),
                          padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                          decoration: BoxDecoration(
                            color: on ? accent.withValues(alpha: .16) : p.surface2,
                            borderRadius: BorderRadius.circular(99),
                            border: Border.all(color: on ? accent : p.border),
                          ),
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Icon(on ? Icons.check_circle : sp.icon, size: 16, color: on ? p.accentInk : p.muted),
                              const SizedBox(width: 7),
                              Text(
                                sp.name,
                                style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500, color: on ? p.text : p.muted),
                              ),
                            ],
                          ),
                        ),
                      ),
                    );
                  },
                ),
            ],
          ),
        ],
      ),
    );
  }
}
