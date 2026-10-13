import 'dart:async';

import 'package:flutter/material.dart';

import '../../l10n/engine_strings.dart';
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
    'off' when platform.isAndroid => 'Эта сборка не обновляется сама: так бывает у сборки для разработки.',
    // The Linux daemon never installs CoreShift: the package manager does;
    // a release build still announces new versions ('available').
    'off' when platform.isLinux => 'Эта сборка не ищет новые версии: так бывает у сборки для разработки. Обновляйте через пакет.',
    'available' => 'Доступна новая версия ${u.label}. Скачайте пакет для своей системы и установите поверх этой.',
    'off' => 'Эта копия не обновляется сама: так бывает у сборки для разработки или у службы старше 0.3.0.',
    'checking' => 'Проверяю…',
    'downloading' => 'Скачиваю версию ${u.label}…',
    'ready' when u.waiting => 'Скачана версия ${u.label}. Установится сама после отключения VPN.',
    'ready' when platform.isAndroid => 'Скачана версия ${u.label}. Android попросит подтвердить установку.',
    'ready' => 'Скачана версия ${u.label}.',
    'installing' => 'Устанавливаю ${u.label}. CoreShift перезапустится сам.',
    'error' => humanError(u.error),
    _ when u.manual && u.checkedAt != null => 'Установлена последняя версия. Проверено ${when(u.checkedAt!)}. Новые версии ставятся пакетом.',
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
    // The home page sent the user to the DNS fields: the fold opens on them.
    final openDns = state.takeDnsSettingsRequest();

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
                : 'Запускать CoreShift в трее при входе в ${platform.isLinux ? 'систему' : 'Windows'} и сразу подключать выбранный сервер',
            trailing: _switch('auto_connect'),
          ),
          // The engine slows down with the screen off (engine/internal/service/power.go).
          if (platform.isAndroid && state.hasSetting('battery_saving'))
            SettingRow(
              title: 'Экономия батареи',
              description: 'Когда экран выключен или включено энергосбережение, связь с сервером проверяется реже и телефон дольше спит',
              trailing: _switch('battery_saving'),
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
          const PanelTitle('Подключение'),
          // On Android every app goes through the VPN: the switch shows
          // only to undo a proxy-only mode chosen somehow.
          if (androidProxy) ...[
            ProxyOnlyRow(state: state, first: true),
            if (tunOff) ProxyAccessCard(state: state),
          ] else if (!platform.isAndroid || tunOff)
            SettingRow(
              first: true,
              title: 'Все приложения через VPN',
              description: info.tunAvailable
                  ? 'Режим TUN. Если выключить — включите ниже «Системный прокси», и CoreShift сам пропишет прокси в систему. '
                        'Без него через VPN пойдут только программы с ручным прокси SOCKS5 или HTTP 127.0.0.1:${info.proxyPort}'
                  : engineText(info.tunUnavailableCode, null, info.tunUnavailable),
              trailing: _switch('tun', enabled: info.tunAvailable || state.setting('tun', false)),
              enabled: info.tunAvailable || state.setting('tun', false),
            ),
          if (!platform.isAndroid && state.hasSetting('proxy.system'))
            SystemProxyRow(state: state, enabled: tunOff)
          else if (!platform.isAndroid)
            const SettingRow(
              title: 'Системный прокси',
              description: 'Нужна более новая служба CoreShift: обновите приложение, и CoreShift сам пропишет прокси в системе',
              trailing: Switch(value: false, onChanged: null),
              enabled: false,
            ),
          if (state.hasSetting('ipv6'))
            SettingRow(
              first: platform.isAndroid && !androidProxy && state.setting('tun', true),
              title: 'IPv6 через туннель',
              description:
                  'IPv6-трафик тоже идёт через VPN. Если выключить, IPv6 при подключении не работает вовсе и сайты открываются по IPv4. '
                  'Выключите, если какие-то сайты перестали открываться',
              trailing: _switch('ipv6'),
            ),
          if (state.hasSetting('cores.fragment'))
            SettingRow(
              first: platform.isAndroid && !androidProxy && state.setting('tun', true) && !state.hasSetting('ipv6'),
              title: 'Обход блокировок (DPI)',
              description:
                  'Делит начало защищённого соединения с сервером на части, чтобы провайдер не узнал его. '
                  'Включите, если VPN то работает, то нет. Работает в Xray и sing-box, mihomo подключается без этого',
              trailing: _switch('cores.fragment'),
            ),
          if (state.hasSetting('cores.switch_server'))
            SettingRow(
              title: 'Менять сервер, если он не отвечает',
              description:
                  'Если сервер не отвечает, а интернет работает, CoreShift подключит следующий сервер той же подписки, который отвечает. '
                  'Автовыбор от панели провайдера меняет сервер всегда',
              trailing: _switch('cores.switch_server'),
            ),
          if (state.hasSetting('cores.latency_test'))
            SettingRow(
              first: !rowsBefore,
              title: 'Проверка задержки',
              description:
                  'Через сервер — запрос через сам сервер: видно, что он действительно пропускает трафик.\n'
                  'TCP-пинг — только проверка, что порт сервера открыт; быстрее и экономнее для батареи и трафика',
              trailing: Seg<String>(
                value: state.setting('cores.latency_test', 'proxy'),
                // Both words fit a phone's width only without the hints; the text explains them.
                options: isCompact(context)
                    ? const [('proxy', 'Через сервер'), ('ping', 'TCP-пинг')]
                    : const [('proxy', 'Через сервер (точнее)'), ('ping', 'TCP-пинг (быстрее)')],
                onChanged: (v) => state.updateSettings((s) => s['cores']['latency_test'] = v),
              ),
            ),
          if (state.hasSetting('cores.speed_mb'))
            SettingRow(
              first: !rowsBefore && !state.hasSetting('cores.latency_test'),
              title: 'Проверка скорости',
              description:
                  'Сколько данных скачивает один сервер при проверке скорости на странице «Серверы»: проверка идёт до 5 секунд или до этого объёма. '
                  'Больше — точнее, но и трафика уходит больше',
              trailing: Seg<int>(
                value: state.setting<num>('cores.speed_mb', 10).toInt(),
                options: const [(5, '5 МБ'), (10, '10 МБ'), (25, '25 МБ')],
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
        // «Настройки DNS» on the home page brings the user here.
        ScrollIntoViewOnce(request: openDns, child: const SectionLabel('DNS')),
        const DnsHint(),
        SettingRow(
          first: true,
          title: 'DNS через VPN',
          description: 'DoH, DoT, DoQ или обычный адрес: https://1.1.1.1/dns-query, tls://dns.google, 8.8.8.8',
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
          title: 'DNS напрямую',
          description: 'Для сайтов без VPN. Пусто — DNS, который был в системе до подключения',
          descriptionWidget: DnsCheckLine(state: state, which: 'direct'),
          trailing: SavingField(
            value: state.setting('dns.direct', ''),
            width: 230,
            mono: true,
            hint: 'системный',
            onSave: (v) async {
              final err = await state.updateSettings((s) => s['dns']['direct'] = v.trim());
              if (err == null) unawaited(state.checkDns('direct'));
              return err;
            },
          ),
        ),
        SettingRow(title: 'Fake-IP', description: 'Имена резолвятся на стороне сервера — быстрее и без утечек DNS', trailing: _switch('dns.fake_ip')),
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
              description: state.versionMismatchAdvice,
              trailing: SelectableText(
                state.serviceVersionLabel,
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
                // Linux: a new version is downloaded from its page by hand.
                if (state.appUpdate.state == 'available') ...[
                  Btn(label: 'Скачать', icon: Icons.open_in_new, kind: BtnKind.primary, small: true, onPressed: state.openUpdatePage),
                  const SizedBox(width: 8),
                ],
                // A development build on Linux checks nothing.
                if (!(platform.isLinux && state.appUpdate.off))
                  Btn(
                    label: 'Проверить сейчас',
                    small: true,
                    loading: state.appUpdate.busy,
                    onPressed: state.online && !state.appUpdate.off ? state.checkAppUpdate : null,
                  ),
              ],
            ),
          ),
          if (state.hasSetting('app_update.auto') && !platform.isAndroid && !platform.isLinux)
            SettingRow(
              title: 'Устанавливать обновления автоматически',
              description:
                  'Раз в день CoreShift проверяет новую версию и ставит её сам, когда VPN выключен. '
                  'Если VPN включён, установка подождёт.',
              trailing: _switch('app_update.auto'),
            ),
          SettingRow(
            title: 'Отказ от ответственности',
            description: 'Некоммерческая программа «как есть»: серверы и подписки предоставляет ваш провайдер',
            trailing: Btn(label: 'Прочитать', small: true, onPressed: () => showDisclaimer(context)),
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
                Fold(
                  title: 'Дополнительно',
                  sub: 'обновление подписок, User-Agent, DNS-серверы, защита DNS по отдельности',
                  initiallyOpen: openDns,
                  child: expert,
                ),
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
          : 'Программы с собственным DNS-over-TLS перейдут на DNS через туннель',
    ),
    // dns.strict is a Windows setting; the Linux daemon ignores it.
    if (platform.isWindows) ('dns.strict', 'Строгий DNS в Windows', 'Запретить Windows опрашивать DNS других сетевых адаптеров в обход туннеля'),
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
      title: 'Защита от утечек DNS',
      description: platform.isAndroid
          ? 'DNS браузеров и DNS-over-TLS только через VPN. Если в Android «Частный DNS» задан вручную, с защитой сайты перестанут открываться'
          : platform.isWindows
          ? 'DNS браузеров, DNS-over-TLS и DNS других сетевых адаптеров Windows не уходят мимо туннеля'
          : 'DNS браузеров и DNS-over-TLS не уходят мимо туннеля',
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
