import 'package:flutter/material.dart';

import '../../platform/desktop.dart' as desktop;
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../shell.dart';
import '../theme.dart';
import '../widgets.dart';
import 'leak_check.dart';

/// What the self-update is doing, in words.
String _appUpdateText(AppState state) {
  final u = state.appUpdate;
  String when(DateTime t) =>
      '${t.day.toString().padLeft(2, '0')}.${t.month.toString().padLeft(2, '0')} '
      '${t.hour.toString().padLeft(2, '0')}:${t.minute.toString().padLeft(2, '0')}';
  return switch (u.state) {
    'off' when platform.isAndroid => 'Эта сборка не обновляется сама: так бывает у сборки для разработки.',
    'off' => 'Эта копия не обновляется сама: так бывает у сборки для разработки или у службы старше 0.3.0.',
    'checking' => 'Проверяю…',
    'downloading' => 'Скачиваю версию ${u.label}…',
    'ready' when u.waiting => 'Скачана версия ${u.label}. Установится сама после отключения VPN.',
    'ready' when platform.isAndroid => 'Скачана версия ${u.label}. Android попросит подтвердить установку.',
    'ready' => 'Скачана версия ${u.label}.',
    'installing' => 'Устанавливаю ${u.label}. CoreShift перезапустится сам.',
    'error' => humanError(u.error),
    _ => u.checkedAt == null ? 'Проверка ещё не проводилась.' : 'Установлена последняя версия. Проверено ${when(u.checkedAt!)}.',
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

    final general = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Приложение'),
          SettingRow(
            first: true,
            title: 'Автозапуск',
            description: platform.isAndroid
                ? 'Подключать выбранный сервер при включении телефона и при открытии CoreShift'
                : 'Запускать CoreShift в трее при входе в Windows и сразу подключать выбранный сервер',
            trailing: _switch('auto_connect'),
          ),
          if (desktop.canNotify)
            SettingRow(
              title: 'Уведомления',
              description: 'Сообщать о смене ядра, обрывах связи и окончании подписки, когда окно CoreShift свёрнуто или в трее',
              trailing: Switch(value: state.systemNotifications, onChanged: (v) => state.setPref('notifications', v)),
            ),
          SettingRow(
            title: 'Ссылки из буфера обмена',
            description: 'Предлагать добавить подписку, когда скопирована ссылка на неё',
            trailing: Switch(value: state.clipboardImport, onChanged: (v) => state.setPref('clipboard_import', v)),
          ),
          SettingRow(
            title: 'Тема',
            trailing: Seg<ThemeMode>(
              value: themeMode,
              options: const [(ThemeMode.dark, 'Тёмная'), (ThemeMode.light, 'Светлая'), (ThemeMode.system, 'Как в системе')],
              onChanged: onThemeMode,
            ),
          ),
        ],
      ),
    );

    // On Android with every app through the VPN the panel may have nothing
    // to offer: then it is left out.
    final rowsBefore = !platform.isAndroid || !state.setting('tun', true) || state.hasSetting('ipv6') || state.hasSetting('cores.fragment');
    final hasNetwork = rowsBefore || state.hasSetting('dns.block_dot') || state.hasSetting('dns.block_browser_doh');
    final network = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Подключение'),
          // On Android every app goes through the VPN: the switch shows
          // only to undo a proxy-only mode chosen somehow.
          if (!platform.isAndroid || !state.setting('tun', true))
            SettingRow(
              first: true,
              title: 'Все приложения через VPN',
              description: info.tunAvailable
                  ? 'Режим TUN. Если выключить, через VPN пойдут только программы с прокси SOCKS5 127.0.0.1:17890'
                  : info.tunUnavailable,
              trailing: _switch('tun', enabled: info.tunAvailable || state.setting('tun', false)),
            ),
          if (state.hasSetting('ipv6'))
            SettingRow(
              first: platform.isAndroid && state.setting('tun', true),
              title: 'IPv6 через туннель',
              description: 'IPv6-трафик тоже идёт через VPN, а не мимо него. Выключите, если какие-то сайты перестали открываться',
              trailing: _switch('ipv6'),
            ),
          if (state.hasSetting('cores.fragment'))
            SettingRow(
              first: platform.isAndroid && state.setting('tun', true) && !state.hasSetting('ipv6'),
              title: 'Обход блокировок (DPI)',
              description:
                  'Делит начало защищённого соединения с сервером на части, чтобы провайдер не узнал его. '
                  'Включите, если VPN то работает, то нет. Работает в Xray и sing-box, mihomo подключается без этого',
              trailing: _switch('cores.fragment'),
            ),
          _LeakGuard(state: state, first: !rowsBefore),
          // The test of what the switch guards against, beside it.
          LeakCheck(state: state),
        ],
      ),
    );

    // Settings few people touch, folded away under «Дополнительно».
    final expert = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        const SectionLabel('Подписки'),
        SettingRow(
          first: true,
          title: 'Обновлять подписки сами',
          description: 'С интервалом, который задаёт панель провайдера',
          trailing: _switch('updates.auto'),
        ),
        SettingRow(
          title: 'User-Agent',
          description: 'Меняйте, только если провайдер просит указать конкретное приложение',
          trailing: SavingField(
            value: state.setting('updates.user_agent', ''),
            width: 170,
            mono: true,
            hint: info.version.isEmpty || info.version == 'dev' ? 'CoreShift' : 'CoreShift/${info.version}',
            onSave: (v) => state.updateSettings((s) => s['updates']['user_agent'] = v.trim()),
          ),
        ),
        const SizedBox(height: 14),
        const SectionLabel('DNS'),
        SettingRow(
          first: true,
          title: 'DNS через VPN',
          description: 'DoH, DoT, DoQ или обычный адрес: https://1.1.1.1/dns-query, tls://dns.google, 8.8.8.8',
          trailing: SavingField(
            value: state.setting('dns.remote', ''),
            width: 230,
            mono: true,
            onSave: (v) => state.updateSettings((s) => s['dns']['remote'] = v.trim()),
          ),
        ),
        SettingRow(
          title: 'DNS для сайтов без VPN',
          description: 'Пусто — DNS, который был в системе до подключения',
          trailing: SavingField(
            value: state.setting('dns.direct', ''),
            width: 230,
            mono: true,
            hint: 'системный',
            onSave: (v) => state.updateSettings((s) => s['dns']['direct'] = v.trim()),
          ),
        ),
        SettingRow(title: 'Fake-IP', description: 'Имена резолвятся на стороне сервера — быстрее и без утечек DNS', trailing: _switch('dns.fake_ip')),
      ],
    );

    final about = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('CoreShift'),
          SettingRow(
            first: true,
            title: 'Версия',
            description: state.version.commit.isEmpty ? null : 'коммит ${state.version.commit}',
            trailing: SelectableText(
              state.version.known ? state.version.label : info.buildVersion.label,
              style: TextStyle(color: p.muted, fontFamily: monoFont),
            ),
          ),
          // On Android the engine is inside the app: one version.
          if (!platform.isAndroid && state.versionMismatch)
            SettingRow(
              title: 'Версия службы',
              description: 'Отличается от приложения: одно из них обновилось без другого. Переустановите CoreShift целиком.',
              trailing: SelectableText(
                info.buildVersion.label,
                style: TextStyle(color: p.warnInk, fontFamily: monoFont),
              ),
            ),
          SettingRow(
            title: 'Обновления',
            description: _appUpdateText(state),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (state.appUpdate.state == 'ready') ...[
                  Btn(
                    label: platform.isAndroid ? 'Обновить' : 'Установить сейчас',
                    icon: Icons.system_update_alt,
                    kind: BtnKind.primary,
                    small: true,
                    tooltip: state.status.active && !platform.isAndroid ? 'VPN отключится на время установки и подключится снова' : null,
                    onPressed: state.online ? state.installAppUpdate : null,
                  ),
                  const SizedBox(width: 8),
                ],
                Btn(
                  label: 'Проверить сейчас',
                  small: true,
                  loading: state.appUpdate.busy,
                  onPressed: state.online && !state.appUpdate.off ? state.checkAppUpdate : null,
                ),
              ],
            ),
          ),
          if (state.hasSetting('app_update.auto') && !platform.isAndroid)
            SettingRow(
              title: 'Устанавливать обновления автоматически',
              description:
                  'Раз в день CoreShift проверяет новую версию и ставит её сам, когда VPN выключен. '
                  'Если VPN включён, установка подождёт.',
              trailing: _switch('app_update.auto'),
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
                Text('Ядра', style: display(16)),
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
                Text(
                  prio.length == 1 ? 'Работает одно ядро, без переключения при сбое' : 'Если ядро перестанет работать, CoreShift переключится на следующее',
                  style: TextStyle(fontSize: 12, color: p.muted),
                ),
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
                const PageHeader('Настройки'),
                for (final w in [general, if (hasNetwork) network, cores, about]) Padding(padding: const EdgeInsets.only(bottom: 16), child: w),
                Fold(title: 'Дополнительно', sub: 'обновление подписок, User-Agent, DNS-серверы', child: expert),
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

  List<String> get _guards => ['dns.block_browser_doh', 'dns.block_dot', if (!platform.isAndroid) 'dns.strict'].where(state.hasSetting).toList();

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final guards = _guards;
    if (guards.isEmpty) return const SizedBox();
    final on = guards.where((g) => state.setting(g, false)).length;
    final partly = on > 0 && on < guards.length;
    return SettingRow(
      first: first,
      title: 'Защита от утечек DNS',
      description: platform.isAndroid
          ? 'DNS браузеров и DNS-over-TLS только через VPN. Если в Android «Частный DNS» задан вручную, с защитой сайты перестанут открываться'
          : 'DNS браузеров, DNS-over-TLS и DNS других сетевых адаптеров Windows не уходят мимо туннеля',
      descriptionWidget: partly
          ? Text('Включено не всё: $on из ${guards.length}. Включите, чтобы защитить всё', style: TextStyle(fontSize: 12, color: p.warnInk))
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
