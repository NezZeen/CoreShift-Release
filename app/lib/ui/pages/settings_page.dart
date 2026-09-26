import 'package:flutter/material.dart';

import '../../platform/desktop.dart' as desktop;
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/errors.dart';
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
    'off' => 'Эта копия не обновляется сама: так бывает у сборки для разработки или у службы старше 0.3.0.',
    'checking' => 'Проверяю…',
    'downloading' => 'Скачиваю версию ${u.label}…',
    'ready' when u.waiting => 'Скачана версия ${u.label}. Установится сама после отключения VPN.',
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
          const PanelTitle('Общие'),
          SettingRow(
            first: true,
            title: platform.isAndroid ? 'Подключаться при открытии' : 'Подключаться при запуске',
            description: platform.isAndroid
                ? 'Подключить выбранный сервер, как только CoreShift откроется'
                : 'Служба подключит выбранный узел сама, как только запустится вместе с системой',
            trailing: _switch('auto_connect'),
          ),
          if (desktop.canNotify)
            SettingRow(
              title: 'Уведомления',
              description: 'Сообщать о смене ядра и обрывах связи, когда окно CoreShift свёрнуто или в трее',
              trailing: Switch(value: state.systemNotifications, onChanged: (v) => state.setPref('notifications', v)),
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

    final network = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Сеть'),
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
          LeakCheck(state: state),
        ],
      ),
    );

    final dns = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('DNS', sub: 'для опытных — значения по умолчанию подходят большинству'),
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
          SettingRow(
            title: 'DNS браузеров только через VPN',
            description: 'Firefox не включает свой DNS-over-HTTPS сам, а если он включён вручную, его запросы идут через туннель',
            trailing: _switch('dns.block_browser_doh'),
          ),
          SettingRow(title: 'Блокировать DNS-over-TLS (порт 853)', trailing: _switch('dns.block_dot')),
          if (!platform.isAndroid)
            SettingRow(
              title: 'Строгий DNS в Windows',
              description: 'Запретить Windows опрашивать DNS других сетевых адаптеров в обход туннеля',
              trailing: _switch('dns.strict'),
            ),
        ],
      ),
    );

    final subs = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Подписки'),
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
              onSave: (v) => state.updateSettings((s) => s['updates']['interval_hours'] = int.tryParse(v) ?? 0),
            ),
          ),
          SettingRow(
            title: 'User-Agent',
            description: 'Меняйте, только если провайдер просит указать конкретное приложение',
            trailing: SavingField(
              value: state.setting('updates.user_agent', ''),
              width: 170,
              mono: true,
              hint: 'CoreShift/0.1',
              onSave: (v) => state.updateSettings((s) => s['updates']['user_agent'] = v.trim()),
            ),
          ),
          if (state.hasSetting('cores.latency_test'))
            SettingRow(
              title: 'Проверка пинга',
              description: state.setting('cores.latency_test', 'ping') == 'proxy'
                  ? 'Запрос через ядро: реальная задержка с учётом шифрования, заодно видно, работает ли узел. Цифры больше пинга'
                  : 'ICMP-пинг до сервера, где он закрыт — время TCP-подключения. Узлы, которые не ответили, проверяются через ядро',
              trailing: Seg<String>(
                value: state.setting('cores.latency_test', 'ping'),
                options: const [('ping', 'Пинг'), ('proxy', 'Через ядро')],
                onChanged: (v) => state.updateSettings((s) => s['cores']['latency_test'] = v),
              ),
            ),
        ],
      ),
    );

    final about = Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('О программе'),
          SettingRow(
            first: true,
            title: 'Версия приложения',
            description: state.version.commit.isEmpty ? null : 'коммит ${state.version.commit}',
            trailing: SelectableText(
              state.version.label,
              style: TextStyle(color: p.muted, fontFamily: monoFont),
            ),
          ),
          SettingRow(
            title: 'Версия службы',
            description: state.versionMismatch
                ? 'Отличается от приложения: одно из них обновилось без другого. Переустановите CoreShift целиком.'
                : (info.commit.isEmpty ? null : 'коммит ${info.commit}'),
            trailing: SelectableText(
              info.buildVersion.label,
              style: TextStyle(color: state.versionMismatch ? warnColor : p.muted, fontFamily: monoFont),
            ),
          ),
          if (!platform.isAndroid)
            SettingRow(
              title: 'Обновления',
              description: _appUpdateText(state),
              trailing: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  if (state.appUpdate.state == 'ready') ...[
                    Btn(
                      label: 'Установить сейчас',
                      icon: Icons.system_update_alt,
                      kind: BtnKind.primary,
                      small: true,
                      tooltip: state.status.active ? 'VPN отключится на время установки и подключится снова' : null,
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
          if (state.updateNotice.isNotEmpty)
            SettingRow(
              title: 'После запуска',
              description: state.updateNotice,
              trailing: const Icon(Icons.system_update_alt, size: 18, color: okColor),
            ),
          if (!platform.isAndroid)
            SettingRow(
              title: 'Служба',
              trailing: Text(
                state.backend.description,
                style: TextStyle(color: p.muted, fontFamily: monoFont),
              ),
            ),
          SettingRow(
            title: 'Ядра',
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final k in allCores) ...[
                  Tooltip(
                    message: '${coreStyle(k).name}: ${info.installed(k) ? 'установлено' : 'нет'}',
                    child: CoreLogo(k, size: 22, off: !info.installed(k)),
                  ),
                  const SizedBox(width: 5),
                ],
              ],
            ),
          ),
        ],
      ),
    );

    return PageFrame(
      children: [
        const PageHeader('Настройки'),
        LayoutBuilder(
          builder: (context, c) {
            if (c.maxWidth < 900) {
              return Column(
                children: [
                  for (final w in [general, network, subs, dns, about]) Padding(padding: const EdgeInsets.only(bottom: 18), child: w),
                ],
              );
            }
            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(child: Column(children: [general, const SizedBox(height: 18), subs, const SizedBox(height: 18), about])),
                const SizedBox(width: 18),
                Expanded(child: Column(children: [network, const SizedBox(height: 18), dns])),
              ],
            );
          },
        ),
      ],
    );
  }
}
