import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../l10n/strings.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../theme.dart';
import '../widgets.dart';

// The proxy without TUN, in «Подключение»: on a computer «Системный прокси»,
// which has CoreShift set the proxy of the system; on a phone «Прокси без
// VPN» and what apps need to use it (engine/internal/service/proxymode.go).

/// «Системный прокси» (Windows, Linux), shown while TUN is off.
class SystemProxyRow extends StatelessWidget {
  final AppState state;

  const SystemProxyRow({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    return SettingRow(
      title: tr('proxy.system.title'),
      description: tr('proxy.system.desc'),
      trailing: Switch(value: state.setting('proxy.system', false) == true, onChanged: (v) => state.updateSettings((s) => (s['proxy'] as Map)['system'] = v)),
    );
  }
}

/// «Прокси без VPN» (Android): the switch, the reverse of «все приложения
/// через VPN».
class ProxyOnlyRow extends StatelessWidget {
  final AppState state;
  final bool first;

  const ProxyOnlyRow({super.key, required this.state, this.first = false});

  @override
  Widget build(BuildContext context) {
    final proxyOnly = state.setting('tun', true) != true;
    return SettingRow(
      first: first,
      title: tr('proxy.only.title'),
      description: tr('proxy.only.desc'),
      trailing: Switch(
        value: proxyOnly,
        // Back to the VPN only where TUN works.
        onChanged: proxyOnly && !state.info.tunAvailable ? null : (v) => state.updateSettings((s) => s['tun'] = !v),
      ),
    );
  }
}

/// The proxy's address and credentials, to copy into apps, and its HTTP
/// door, shown on Android while «Прокси без VPN» is on.
class ProxyAccessCard extends StatefulWidget {
  final AppState state;

  const ProxyAccessCard({super.key, required this.state});

  @override
  State<ProxyAccessCard> createState() => _ProxyAccessCardState();
}

/// The link that adds the proxy to Telegram.
String telegramProxyLink({required int port, required String user, required String pass}) =>
    'tg://socks?server=127.0.0.1&port=$port&user=${Uri.encodeQueryComponent(user)}&pass=${Uri.encodeQueryComponent(pass)}';

/// New random credentials of the proxy, as the engine makes them.
(String, String) newProxyCredentials([Random? random]) {
  final r = random ?? Random.secure();
  String pick(int n, String alphabet) => String.fromCharCodes(List.generate(n, (_) => alphabet.codeUnitAt(r.nextInt(alphabet.length))));
  return ('cs${pick(6, 'abcdefghijklmnopqrstuvwxyz0123456789')}', pick(20, 'ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789'));
}

class _ProxyAccessCardState extends State<ProxyAccessCard> {
  bool _showPass = false;

  AppState get state => widget.state;

  void _copy(String what, String value) {
    Clipboard.setData(ClipboardData(text: value));
    state.toast(tr('proxy.copied', {'what': what}), ToastKind.ok);
  }

  Future<void> _telegram(String user, String pass) async {
    final ok = await platform.openUrl(telegramProxyLink(port: state.info.proxyPort, user: user, pass: pass));
    if (!ok) state.toast(tr('proxy.telegram.failed'), ToastKind.err);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final user = state.setting('proxy.user', '');
    final pass = state.setting('proxy.pass', '');
    final port = state.info.proxyPort;
    final openHTTP = state.setting('proxy.open_http', false) == true;

    Widget field(String label, String value, {String? shown, Widget? extra, required String copied}) => Padding(
      padding: const EdgeInsets.symmetric(vertical: 3),
      child: Row(
        children: [
          SizedBox(
            width: 70,
            child: Text(label, style: TextStyle(fontSize: 12, color: p.muted)),
          ),
          Expanded(
            child: SelectableText(shown ?? value, style: const TextStyle(fontFamily: 'monospace', fontSize: 13)),
          ),
          ?extra,
          Btn(icon: Icons.copy, small: true, kind: BtnKind.ghost, tooltip: tr('proxy.copy'), onPressed: value.isEmpty ? null : () => _copy(copied, value)),
        ],
      ),
    );

    return Container(
      key: const ValueKey('proxy-access'),
      margin: const EdgeInsets.only(top: 4, bottom: 10),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: p.surface2,
        borderRadius: BorderRadius.circular(10),
        border: Border.all(color: p.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(tr('proxy.access.title'), style: const TextStyle(fontWeight: FontWeight.w600)),
          const SizedBox(height: 6),
          field(tr('proxy.access.address'), '127.0.0.1', copied: tr('proxy.access.address')),
          field(tr('proxy.access.port'), '$port', copied: tr('proxy.access.port')),
          if (user.isEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 4),
              child: Text(tr('proxy.access.no_credentials'), style: TextStyle(fontSize: 12, color: p.warnInk)),
            )
          else ...[
            field(tr('proxy.access.login'), user, copied: tr('proxy.access.login')),
            field(
              tr('proxy.access.password'),
              pass,
              shown: _showPass ? pass : '•' * pass.length.clamp(8, 20),
              copied: tr('proxy.access.password'),
              extra: Btn(
                icon: _showPass ? Icons.visibility_off : Icons.visibility,
                small: true,
                kind: BtnKind.ghost,
                tooltip: tr(_showPass ? 'proxy.access.hide' : 'proxy.access.show'),
                onPressed: () => setState(() => _showPass = !_showPass),
              ),
            ),
          ],
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              Btn(
                label: tr('proxy.access.telegram'),
                icon: Icons.send,
                kind: BtnKind.primary,
                small: true,
                onPressed: user.isEmpty ? null : () => _telegram(user, pass),
              ),
              Btn(
                label: tr('proxy.access.new_password'),
                icon: Icons.refresh,
                small: true,
                kind: BtnKind.ghost,
                onPressed: () {
                  final (u, pw) = newProxyCredentials();
                  state.updateSettings(
                    (s) => (s['proxy'] as Map)
                      ..['user'] = u
                      ..['pass'] = pw,
                  );
                },
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text(tr('proxy.access.howto', {'port': port}), style: TextStyle(fontSize: 12, color: p.muted)),
          const SizedBox(height: 4),
          Text(tr('proxy.access.wifi'), style: TextStyle(fontSize: 12, color: p.muted)),
          SettingRow(
            title: tr('proxy.open_http.title'),
            description: tr('proxy.open_http.desc'),
            descriptionWidget: openHTTP ? Text(tr('proxy.open_http.on'), style: TextStyle(fontSize: 12, color: p.warnInk)) : null,
            trailing: Switch(value: openHTTP, onChanged: (v) => state.updateSettings((s) => (s['proxy'] as Map)['open_http'] = v)),
          ),
        ],
      ),
    );
  }
}
