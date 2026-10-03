import 'package:flutter/material.dart';

import '../../platform/desktop.dart' as desktop;
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../shell.dart';
import '../theme.dart';
import '../widgets.dart';

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
    final interval = state.setting('updates.interval_hours', 12);

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
    final hasNetwork = !platform.isAndroid || !state.setting('tun', true) || state.hasSetting('ipv6') || state.hasSetting('cores.fragment');
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
        ],
      ),
    );

    final dns = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('DNS', sub: 'можно не трогать'),
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
          _LeakGuard(state: state),
        ],
      ),
    );

    final subs = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Обновление подписок'),
          SettingRow(
            first: true,
            title: 'Автообновление',
            description: 'С интервалом, который задаёт панель, иначе — каждые $interval ч',
            trailing: _switch('updates.auto'),
          ),
          SettingRow(
            title: 'Обновлять каждые, ч',
            description: 'Если провайдер не указал свой интервал',
            trailing: SavingField(
              value: '$interval',
              width: 64,
              numeric: true,
              align: TextAlign.center,
              enabled: state.setting('updates.auto', false),
              onSave: (v) async {
                final h = int.tryParse(v) ?? 0;
                if (h < 1) return 'Укажите число часов';
                return state.updateSettings((s) => s['updates']['interval_hours'] = h);
              },
            ),
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
        ],
      ),
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
                style: TextStyle(color: warnColor, fontFamily: monoFont),
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

    // For the curious: the cores and the journal as pages of their own, the
    // subscription and DNS details folded away.
    final advanced = Panel(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _LinkRow(
            icon: Icons.memory,
            title: 'Ядра',
            text: 'Xray, sing-box и mihomo: порядок и переключение при сбоях',
            onTap: () => Nav.to(context, PageId.cores),
          ),
          Divider(height: 1, color: p.border),
          _LinkRow(icon: Icons.receipt_long_outlined, title: 'Журнал', text: 'События службы и ядер для поддержки', onTap: () => Nav.to(context, PageId.logs)),
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
                for (final w in [general, if (hasNetwork) network, about]) Padding(padding: const EdgeInsets.only(bottom: 16), child: w),
                const Padding(padding: EdgeInsets.fromLTRB(4, 10, 4, 4), child: SectionLabel('Для опытных')),
                advanced,
                const SizedBox(height: 8),
                _More(children: [subs, const SizedBox(height: 16), dns]),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

/// A line that opens another page.
class _LinkRow extends StatelessWidget {
  final IconData icon;
  final String title;
  final String text;
  final VoidCallback onTap;
  const _LinkRow({required this.icon, required this.title, required this.text, required this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return InkWell(
      onTap: onTap,
      hoverColor: p.surface2,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
        child: Row(
          children: [
            Icon(icon, size: 20, color: p.muted),
            const SizedBox(width: 14),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(title, style: const TextStyle(fontWeight: FontWeight.w600)),
                  Text(
                    text,
                    style: TextStyle(fontSize: 12, color: p.muted),
                    overflow: TextOverflow.ellipsis,
                    maxLines: 2,
                  ),
                ],
              ),
            ),
            Icon(Icons.chevron_right, color: p.dim),
          ],
        ),
      ),
    );
  }
}

/// One switch for the guards against DNS going round the tunnel; each can
/// still be set on its own under "Подробнее".
class _LeakGuard extends StatefulWidget {
  final AppState state;
  const _LeakGuard({required this.state});

  @override
  State<_LeakGuard> createState() => _LeakGuardState();
}

class _LeakGuardState extends State<_LeakGuard> {
  bool open = false;

  AppState get s => widget.state;

  List<(String, String, String?)> get _guards => [
    (
      'dns.block_browser_doh',
      'DNS браузеров только через VPN',
      'Firefox не включает свой DNS-over-HTTPS сам, а если он включён вручную, его запросы идут через туннель',
    ),
    (
      'dns.block_dot',
      'Блокировать DNS-over-TLS (порт 853)',
      platform.isAndroid
          ? 'Android в режиме «Частный DNS: автоматически» перейдёт на обычный DNS. Если «Частный DNS» задан вручную, сайты перестанут открываться'
          : null,
    ),
    if (!platform.isAndroid) ('dns.strict', 'Строгий DNS в Windows', 'Запретить Windows опрашивать DNS других сетевых адаптеров в обход туннеля'),
  ].where((g) => s.hasSetting(g.$1)).toList();

  Future<String?> _set(Iterable<String> paths, bool v) => s.updateSettings((x) {
    for (final path in paths) {
      final keys = path.split('.');
      Map m = x;
      for (final k in keys.take(keys.length - 1)) {
        m = m[k] as Map;
      }
      m[keys.last] = v;
    }
  });

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final guards = _guards;
    if (guards.isEmpty) return const SizedBox();
    final on = guards.where((g) => s.setting(g.$1, false)).length;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SettingRow(
          title: 'Защита от утечек DNS',
          descriptionWidget: Wrap(
            crossAxisAlignment: WrapCrossAlignment.center,
            spacing: 6,
            children: [
              Text(
                on == 0
                    ? 'Выключена'
                    : on == guards.length
                    ? 'DNS-запросы браузеров и системы не уходят мимо туннеля'
                    : 'Включено не всё: $on из ${guards.length}',
                style: TextStyle(fontSize: 12, color: on > 0 && on < guards.length ? warnColor : p.muted),
              ),
              InkWell(
                onTap: () => setState(() => open = !open),
                child: Text(
                  open ? 'Свернуть' : 'Подробнее',
                  style: TextStyle(fontSize: 12, color: p.accentInk, fontWeight: FontWeight.w600),
                ),
              ),
            ],
          ),
          trailing: Switch(value: on == guards.length, onChanged: (v) => _set(guards.map((g) => g.$1), v)),
        ),
        if (open)
          Padding(
            padding: const EdgeInsets.only(left: 16),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                for (final (path, title, text) in guards)
                  SettingRow(
                    title: title,
                    description: text,
                    trailing: Switch(value: s.setting(path, false), onChanged: (v) => _set([path], v)),
                  ),
              ],
            ),
          ),
      ],
    );
  }
}

/// Settings few people touch, folded away.
class _More extends StatefulWidget {
  final List<Widget> children;
  const _More({required this.children});

  @override
  State<_More> createState() => _MoreState();
}

class _MoreState extends State<_More> {
  bool open = false;

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        InkWell(
          borderRadius: BorderRadius.circular(12),
          onTap: () => setState(() => open = !open),
          child: Container(
            padding: const EdgeInsets.symmetric(vertical: 12, horizontal: 18),
            decoration: BoxDecoration(
              borderRadius: BorderRadius.circular(12),
              border: Border.all(color: p.border),
            ),
            child: Row(
              children: [
                Icon(Icons.dns_outlined, size: 20, color: p.muted),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      const Text('Подписки и DNS', style: TextStyle(fontWeight: FontWeight.w600)),
                      Text(
                        'Частота обновления, User-Agent, DNS-серверы и защита от утечек',
                        style: TextStyle(fontSize: 12, color: p.muted),
                        overflow: TextOverflow.ellipsis,
                        maxLines: 2,
                      ),
                    ],
                  ),
                ),
                Icon(open ? Icons.expand_less : Icons.expand_more, color: p.muted),
              ],
            ),
          ),
        ),
        if (open) ...[const SizedBox(height: 8), ...widget.children],
      ],
    );
  }
}
