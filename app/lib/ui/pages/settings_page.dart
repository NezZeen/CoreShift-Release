import 'dart:async';

import 'package:flutter/material.dart';

import '../../l10n/strings.dart';
import '../../platform/desktop.dart' as desktop;
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../checkup.dart';
import '../disclaimer.dart';
import '../shell.dart';
import '../theme.dart';
import '../widgets.dart';
import 'dns_check.dart';
import 'leak_check.dart';
import 'proxy_mode.dart';

/// What the self-update is doing, in words.
String _appUpdateText(AppState state) {
  final u = state.appUpdate;
  String when(DateTime t) =>
      '${t.day.toString().padLeft(2, '0')}.${t.month.toString().padLeft(2, '0')} '
      '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
  return switch (u.state) {
    'off' when platform.isAndroid => tr('settings.update.off.android'),
    // The Linux daemon never installs CoreShift: the package manager does;
    // a release build still announces new versions ('available').
    'off' when platform.isLinux => tr('settings.update.off.linux'),
    'available' => tr('settings.update.available', {'label': u.label}),
    'off' => tr('settings.update.off'),
    'checking' => tr('settings.update.checking'),
    'downloading' => tr('settings.update.downloading', {'label': u.label}),
    'ready' when u.waiting => tr('settings.update.ready.waiting', {'label': u.label}),
    'ready' when platform.isAndroid => tr('settings.update.ready.android', {'label': u.label}),
    'ready' => tr('settings.update.ready', {'label': u.label}),
    'installing' => tr('settings.update.installing', {'label': u.label}),
    'error' => humanError(u.error),
    _ when u.manual && u.checkedAt != null => tr('settings.update.latest.manual', {'when': when(u.checkedAt!)}),
    _ => u.checkedAt == null ? tr('settings.update.never') : tr('settings.update.latest', {'when': when(u.checkedAt!)}),
  };
}

class SettingsPage extends StatelessWidget {
  final AppState state;
  final ThemeMode themeMode;
  final ValueChanged<ThemeMode> onThemeMode;

  const SettingsPage({super.key, required this.state, required this.themeMode, required this.onThemeMode});

  Widget _switch(String path, {bool enabled = true}) {
    final keys = path.split('.');
    return Switch(
      value: state.setting(path, false),
      onChanged: enabled
          ? (v) => state.updateSettings((s) {
              Map m = s;
              for (final k in keys.take(keys.length - 1)) {
                m = m[k] as Map;
              }
              m[keys.last] = v;
            })
          : null,
    );
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final info = state.info;
    // The home page sent the user to the DNS fields: the fold opens on them.
    final openDns = state.takeDnsSettingsRequest();

    final general = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle(tr('settings.app.title')),
          SettingRow(
            first: true,
            title: tr('settings.autostart.title'),
            description: platform.isAndroid
                ? tr('settings.autostart.desc.android')
                : tr(platform.isLinux ? 'settings.autostart.desc.linux' : 'settings.autostart.desc.windows'),
            trailing: _switch('auto_connect'),
          ),
          // The engine slows down with the screen off (engine/internal/service/power.go).
          if (platform.isAndroid && state.hasSetting('battery_saving'))
            SettingRow(title: tr('settings.battery.title'), description: tr('settings.battery.desc'), trailing: _switch('battery_saving')),
          if (desktop.canNotify)
            SettingRow(
              title: tr('settings.notifications.title'),
              description: tr('settings.notifications.desc'),
              trailing: Switch(value: state.systemNotifications, onChanged: (v) => state.setPref('notifications', v)),
            ),
          SettingRow(
            title: tr('settings.clipboard.title'),
            description: tr('settings.clipboard.desc'),
            trailing: Switch(value: state.clipboardImport, onChanged: (v) => state.setPref('clipboard_import', v)),
          ),
          SettingRow(
            title: tr('settings.theme.title'),
            trailing: Seg<ThemeMode>(
              value: themeMode,
              options: [
                (ThemeMode.dark, tr('settings.theme.dark')),
                (ThemeMode.light, tr('settings.theme.light')),
                (ThemeMode.system, tr('settings.theme.system')),
              ],
              onChanged: onThemeMode,
            ),
          ),
          if (englishReady)
            SettingRow(
              title: tr('settings.language'),
              trailing: Seg<String>(
                value: state.languagePref,
                options: [('system', tr('settings.language.system')), ('ru', tr('settings.language.ru')), ('en', tr('settings.language.en'))],
                onChanged: state.setLanguage,
              ),
            ),
        ],
      ),
    );

    // On Android with every app through the VPN the panel may have nothing
    // to offer: then it is left out.
    // «Прокси без VPN» on Android, with an engine that knows it.
    final androidProxy = platform.isAndroid && state.hasSetting('proxy.user');
    final tunOff = !state.setting('tun', true);
    final rowsBefore =
        !platform.isAndroid ||
        androidProxy ||
        tunOff ||
        state.hasSetting('ipv6') ||
        state.hasSetting('cores.fragment') ||
        state.hasSetting('cores.switch_server');
    final hasNetwork = rowsBefore || state.hasSetting('dns.block_dot') || state.hasSetting('dns.block_browser_doh');
    final network = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle(tr('settings.network.title')),
          // On Android every app goes through the VPN: the switch shows
          // only to undo a proxy-only mode chosen somehow.
          if (androidProxy) ...[
            ProxyOnlyRow(state: state, first: true),
            if (tunOff) ProxyAccessCard(state: state),
          ] else if (!platform.isAndroid || tunOff)
            SettingRow(
              first: true,
              title: tr('settings.tun.title'),
              description: info.tunAvailable ? tr('settings.tun.desc', {'port': info.proxyPort}) : info.tunUnavailable,
              trailing: _switch('tun', enabled: info.tunAvailable || state.setting('tun', false)),
            ),
          if (!platform.isAndroid && tunOff && state.hasSetting('proxy.system')) SystemProxyRow(state: state),
          if (state.hasSetting('ipv6'))
            SettingRow(
              first: platform.isAndroid && !androidProxy && state.setting('tun', true),
              title: tr('settings.ipv6.title'),
              description: tr('settings.ipv6.desc'),
              trailing: _switch('ipv6'),
            ),
          if (state.hasSetting('cores.fragment'))
            SettingRow(
              first: platform.isAndroid && !androidProxy && state.setting('tun', true) && !state.hasSetting('ipv6'),
              title: tr('settings.dpi.title'),
              description: tr('settings.dpi.desc'),
              trailing: _switch('cores.fragment'),
            ),
          if (state.hasSetting('cores.switch_server'))
            SettingRow(title: tr('settings.switch_server.title'), description: tr('settings.switch_server.desc'), trailing: _switch('cores.switch_server')),
          if (state.hasSetting('cores.latency_test'))
            SettingRow(
              first: !rowsBefore,
              title: tr('settings.latency.title'),
              description: tr('settings.latency.desc'),
              trailing: Seg<String>(
                value: state.setting('cores.latency_test', 'proxy'),
                // Both words fit a phone's width only without the hints; the text explains them.
                options: isCompact(context)
                    ? [('proxy', tr('settings.latency.proxy')), ('ping', tr('settings.latency.ping'))]
                    : [('proxy', tr('settings.latency.proxy.long')), ('ping', tr('settings.latency.ping.long'))],
                onChanged: (v) => state.updateSettings((s) => s['cores']['latency_test'] = v),
              ),
            ),
          if (state.hasSetting('cores.speed_mb'))
            SettingRow(
              first: !rowsBefore && !state.hasSetting('cores.latency_test'),
              title: tr('settings.speed.title'),
              description: tr('settings.speed.desc'),
              trailing: Seg<int>(
                value: state.setting<num>('cores.speed_mb', 10).toInt(),
                options: [
                  for (final n in const [5, 10, 25]) (n, tr('settings.speed.mb', {'n': n})),
                ],
                onChanged: (v) => state.updateSettings((s) => s['cores']['speed_mb'] = v),
              ),
            ),
          _LeakGuard(state: state, first: !rowsBefore && !state.hasSetting('cores.latency_test') && !state.hasSetting('cores.speed_mb')),
          // The test of what the switch guards against, beside it.
          LeakCheck(state: state),
          // Every check at once, the leak test among them.
          CheckupSettingRow(state: state),
        ],
      ),
    );

    // Settings few people touch, folded away under «Дополнительно».
    final expert = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SectionLabel(tr('settings.subs.label')),
        SettingRow(first: true, title: tr('settings.subs.auto.title'), description: tr('settings.subs.auto.desc'), trailing: _switch('updates.auto')),
        SettingRow(
          title: tr('settings.ua.title'),
          description: tr('settings.ua.desc'),
          trailing: SavingField(
            value: state.setting('updates.user_agent', ''),
            width: 170,
            mono: true,
            hint: info.version.isEmpty || info.version == 'dev' ? 'CoreShift' : 'CoreShift/${info.version}',
            onSave: (v) => state.updateSettings((s) => s['updates']['user_agent'] = v.trim()),
          ),
        ),
        const SizedBox(height: 14),
        // «Настройки DNS» on the home page brings the user here.
        ScrollIntoViewOnce(request: openDns, child: const SectionLabel('DNS')),
        const DnsHint(),
        SettingRow(
          first: true,
          title: tr('settings.dns.remote.title'),
          description: tr('settings.dns.remote.desc'),
          descriptionWidget: DnsCheckLine(state: state, which: 'remote'),
          trailing: SavingField(
            value: state.setting('dns.remote', ''),
            width: 230,
            mono: true,
            onSave: (v) async {
              final err = await state.updateSettings((s) => s['dns']['remote'] = v.trim());
              if (err == null) unawaited(state.checkDns('remote'));
              return err;
            },
          ),
        ),
        SettingRow(
          title: tr('settings.dns.direct.title'),
          description: tr('settings.dns.direct.desc'),
          descriptionWidget: DnsCheckLine(state: state, which: 'direct'),
          trailing: SavingField(
            value: state.setting('dns.direct', ''),
            width: 230,
            mono: true,
            hint: tr('settings.dns.direct.hint'),
            onSave: (v) async {
              final err = await state.updateSettings((s) => s['dns']['direct'] = v.trim());
              if (err == null) unawaited(state.checkDns('direct'));
              return err;
            },
          ),
        ),
        SettingRow(title: tr('settings.fakeip.title'), description: tr('settings.fakeip.desc'), trailing: _switch('dns.fake_ip')),
        // Each guard of «Защита от утечек DNS» on its own, for when one of
        // them gets in the way.
        for (final (path, title, description) in _LeakGuard.parts)
          if (state.hasSetting(path)) SettingRow(title: title, description: description, trailing: _switch(path)),
      ],
    );

    final about = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('CoreShift'),
          SettingRow(
            first: true,
            title: tr('settings.version.title'),
            description: state.version.commit.isEmpty ? null : tr('settings.version.commit', {'commit': state.version.commit}),
            trailing: SelectableText(
              state.version.known ? state.version.label : info.buildVersion.label,
              style: TextStyle(color: p.muted, fontFamily: monoFont),
            ),
          ),
          // On Android the engine is inside the app: one version.
          if (!platform.isAndroid && state.versionMismatch)
            SettingRow(
              title: tr('settings.service_version.title'),
              description: state.versionMismatchAdvice,
              trailing: SelectableText(
                state.serviceVersionLabel,
                style: TextStyle(color: p.warnInk, fontFamily: monoFont),
              ),
            ),
          SettingRow(
            title: tr('settings.updates.title'),
            description: _appUpdateText(state),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (state.appUpdate.state == 'ready') ...[
                  Btn(
                    label: tr(platform.isAndroid ? 'settings.updates.install.android' : 'settings.updates.install'),
                    icon: Icons.system_update_alt,
                    kind: BtnKind.primary,
                    small: true,
                    tooltip: state.status.active && !platform.isAndroid ? tr('settings.updates.install.tip') : null,
                    onPressed: state.online ? state.installAppUpdate : null,
                  ),
                  const SizedBox(width: 8),
                ],
                // Linux: a new version is downloaded from its page by hand.
                if (state.appUpdate.state == 'available') ...[
                  Btn(label: tr('settings.updates.download'), icon: Icons.open_in_new, kind: BtnKind.primary, small: true, onPressed: state.openUpdatePage),
                  const SizedBox(width: 8),
                ],
                // A development build on Linux checks nothing.
                if (!(platform.isLinux && state.appUpdate.off))
                  Btn(
                    label: tr('settings.updates.check'),
                    small: true,
                    loading: state.appUpdate.busy,
                    onPressed: state.online && !state.appUpdate.off ? state.checkAppUpdate : null,
                  ),
              ],
            ),
          ),
          if (state.hasSetting('app_update.auto') && !platform.isAndroid && !platform.isLinux)
            SettingRow(title: tr('settings.updates.auto.title'), description: tr('settings.updates.auto.desc'), trailing: _switch('app_update.auto')),
          SettingRow(
            title: tr('settings.disclaimer.title'),
            description: tr('settings.disclaimer.desc'),
            trailing: Btn(label: tr('settings.disclaimer.read'), small: true, onPressed: () => showDisclaimer(context)),
          ),
        ],
      ),
    );

    // The cores: what is used, in what order, and whether a newer version
    // waits. The whole list is a page of its own, opened from here only.
    final prio = state.setting('cores.mode', 'auto') == 'manual'
        ? [state.setting('cores.manual', '')]
        : state.setting<List>('cores.priority', const []).cast<String>();
    final cores = Panel(
      onTap: () => Nav.to(context, PageId.cores),
      child: Row(
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(tr('settings.cores.title'), style: display(16)),
                const SizedBox(height: 10),
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    for (final (i, k) in prio.indexed) ...[
                      if (i > 0) Icon(Icons.chevron_right, size: 16, color: p.dim),
                      CoreLogo(k, size: 22),
                      Text(coreStyle(k).name, style: const TextStyle(fontWeight: FontWeight.w500)),
                    ],
                  ],
                ),
                const SizedBox(height: 8),
                Text(tr(prio.length == 1 ? 'settings.cores.single' : 'settings.cores.chain'), style: TextStyle(fontSize: 12, color: p.muted)),
              ],
            ),
          ),
          const SizedBox(width: 10),
          Icon(Icons.chevron_right, color: p.dim),
        ],
      ),
    );

    // One column of sections, the most used first: a settings page is
    // read from the top, not scanned across.
    return PageFrame(
      children: [
        Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 780),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                PageHeader(tr('settings.title')),
                for (final w in [general, if (hasNetwork) network, cores, about]) Padding(padding: const EdgeInsets.only(bottom: 16), child: w),
                Fold(title: tr('settings.more.title'), sub: tr('settings.more.sub'), initiallyOpen: openDns, child: expert),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

/// One switch for the guards against DNS going round the tunnel: browsers'
/// own DNS, DNS-over-TLS and, on Windows, the other adapters' DNS. On sets
/// every guard, so none is weakened by the single control.
class _LeakGuard extends StatelessWidget {
  final AppState state;
  final bool first;
  const _LeakGuard({required this.state, this.first = false});

  /// The guards one by one, as «Дополнительно» lists them.
  static List<(String, String, String)> get parts => [
    ('dns.block_browser_doh', tr('settings.guard.doh.title'), tr('settings.guard.doh.desc')),
    ('dns.block_dot', tr('settings.guard.dot.title'), tr(platform.isAndroid ? 'settings.guard.dot.desc.android' : 'settings.guard.dot.desc')),
    // dns.strict is a Windows setting; the Linux daemon ignores it.
    if (platform.isWindows) ('dns.strict', tr('settings.guard.strict.title'), tr('settings.guard.strict.desc')),
  ];

  List<String> get _guards => [for (final (path, _, _) in parts) path].where(state.hasSetting).toList();

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final guards = _guards;
    if (guards.isEmpty) return const SizedBox();
    final on = guards.where((g) => state.setting(g, false)).length;
    final partly = on > 0 && on < guards.length;
    return SettingRow(
      first: first,
      title: tr('settings.guard.title'),
      description: platform.isAndroid
          ? tr('settings.guard.desc.android')
          : platform.isWindows
          ? tr('settings.guard.desc.windows')
          : tr('settings.guard.desc'),
      descriptionWidget: partly
          ? Text(tr('settings.guard.partly', {'on': on, 'total': guards.length}), style: TextStyle(fontSize: 12, color: p.warnInk))
          : null,
      trailing: Switch(
        value: on == guards.length,
        onChanged: (v) => state.updateSettings((x) {
          for (final path in guards) {
            final keys = path.split('.');
            (x[keys.first] as Map)[keys.last] = v;
          }
        }),
      ),
    );
  }
}
