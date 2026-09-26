import 'package:flutter/material.dart';

import '../../api/models.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../theme.dart';
import '../widgets.dart';

class CoresPage extends StatelessWidget {
  final AppState state;
  const CoresPage({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    final mode = state.setting('cores.mode', 'auto');
    return PageFrame(
      children: [
        PageHeader(
          'Ядра',
          subtitle: 'Установленные ядра, их приоритет и правила автоматического переключения.',
          actions: [
            // Android runs programs only from the APK: cores update with it.
            if (!platform.isAndroid)
              Btn(
                label: 'Проверить обновления',
                icon: Icons.system_update_alt,
                loading: state.checkingUpdates,
                onPressed: state.updatingCore.isNotEmpty || !state.online ? null : state.checkCoreUpdates,
              ),
            Seg<String>(
              value: mode,
              options: const [('auto', 'Автосвап'), ('manual', 'Вручную')],
              onChanged: (v) => state.updateSettings((s) {
                s['cores']['mode'] = v;
                if (v == 'manual' && (s['cores']['manual'] ?? '') == '') {
                  s['cores']['manual'] = (s['cores']['priority'] as List).cast<String>().firstWhere(state.info.installed, orElse: () => 'xray');
                }
              }),
            ),
          ],
        ),
        // On a phone the tiles repeat the priority list below; they stay
        // only for picking the core by hand.
        if (!isCompact(context) || mode == 'manual')
          LayoutBuilder(
            builder: (context, c) {
              final cols = c.maxWidth >= 900 ? 3 : 1;
              final w = (c.maxWidth - 12 * (cols - 1)) / cols;
              return Wrap(
                spacing: 12,
                runSpacing: 12,
                children: [
                  for (final k in allCores)
                    SizedBox(
                      width: w,
                      child: _CoreTile(state: state, kind: k),
                    ),
                ],
              );
            },
          ),
        if (mode == 'manual')
          Padding(
            padding: const EdgeInsets.only(top: 10),
            child: Text(
              'Ручной режим: нажмите на плитку, чтобы выбрать ядро. Переключения при сбоях не будет.',
              style: TextStyle(fontSize: 12, color: context.pal.dim),
            ),
          ),
        const SizedBox(height: 18),
        LayoutBuilder(
          builder: (context, c) {
            final prio = _PriorityCard(state: state);
            final rules = _RulesCard(state: state);
            if (c.maxWidth < 900) return Column(children: [prio, const SizedBox(height: 18), rules]);
            return Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Expanded(flex: 10, child: prio),
                const SizedBox(width: 18),
                Expanded(flex: 12, child: rules),
              ],
            );
          },
        ),
        const SizedBox(height: 18),
        _Matrix(info: state.info),
      ],
    );
  }
}

class _CoreTile extends StatelessWidget {
  final AppState state;
  final String kind;
  const _CoreTile({required this.state, required this.kind});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final s = coreStyle(kind);
    final st = state.status;
    final installed = state.info.installed(kind);
    final manual = state.setting('cores.mode', 'auto') == 'manual';
    final picked = manual && state.setting('cores.manual', '') == kind;
    final running = st.active && st.core == kind;
    final failed = st.active && st.failed.containsKey(kind);
    final prio = state.setting<List>('cores.priority', const []).cast<String>();
    final features = state.info.cores.where((c) => c.kind == kind).firstOrNull?.features ?? const [];
    final protocols = features.where((f) => f.startsWith('protocol:')).length;
    final version = state.info.versionOf(kind);
    final update = state.updateOf(kind);
    final updating = state.updatingCore == kind;

    final (pill, pillColor) = !installed
        ? ('не установлено', p.dim)
        : running
        ? ('работает', okColor)
        : failed
        ? ('сбой', errColor)
        : manual
        ? (picked ? 'выбрано' : 'не используется', picked ? accent : p.dim)
        : prio.contains(kind)
        ? ('приоритет ${prio.indexOf(kind) + 1}', p.muted)
        : ('выключено', p.dim);

    return Panel(
      borderColor: running ? s.color : (picked ? accent : null),
      onTap: manual && installed && !picked ? () => state.updateSettings((x) => x['cores']['manual'] = kind) : null,
      child: Opacity(
        opacity: installed ? 1 : .55,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                CoreLogo(kind, size: 40),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(s.name, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 15)),
                      Text(
                        !installed ? (platform.isAndroid ? 'нет в этой сборке' : 'нет в папке ядер') : (version.isEmpty ? 'установлено' : 'версия $version'),
                        style: TextStyle(fontSize: 12, color: p.muted, fontFamily: version.isEmpty ? null : monoFont),
                      ),
                    ],
                  ),
                ),
                Pill(pill, color: pillColor),
              ],
            ),
            const SizedBox(height: 14),
            Row(
              children: [
                Text('Протоколы', style: TextStyle(fontSize: 12, color: p.muted)),
                const Spacer(),
                Text('$protocols из 8', style: TextStyle(fontSize: 12, color: p.muted)),
              ],
            ),
            const SizedBox(height: 6),
            ClipRRect(
              borderRadius: BorderRadius.circular(9),
              child: LinearProgressIndicator(value: protocols / 8, minHeight: 5, backgroundColor: p.surface3, color: s.color),
            ),
            if (installed && update != null) ...[const SizedBox(height: 12), _UpdateRow(state: state, update: update, updating: updating)],
          ],
        ),
      ),
    );
  }
}

/// Under a core tile once updates were checked: up to date, a newer
/// version with a button, or why the check failed.
class _UpdateRow extends StatelessWidget {
  final AppState state;
  final CoreUpdate update;
  final bool updating;
  const _UpdateRow({required this.state, required this.update, required this.updating});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    if (update.error.isNotEmpty) {
      return Tooltip(
        message: update.error,
        child: Row(
          children: [
            const Icon(Icons.error_outline, size: 15, color: warnColor),
            const SizedBox(width: 6),
            Expanded(
              child: Text(
                humanError(update.error),
                style: TextStyle(fontSize: 12, color: p.muted),
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
            ),
          ],
        ),
      );
    }
    if (!update.available) {
      return Row(
        children: [
          const Icon(Icons.check_circle_outline, size: 15, color: okColor),
          const SizedBox(width: 6),
          Text('Последняя версия', style: TextStyle(fontSize: 12, color: p.muted)),
        ],
      );
    }
    return Row(
      children: [
        const Icon(Icons.new_releases_outlined, size: 15, color: accent),
        const SizedBox(width: 6),
        Expanded(
          child: Text(
            'Доступна ${update.latest}${update.size > 0 ? ' · ${formatBytes(update.size)}' : ''}',
            style: const TextStyle(fontSize: 12),
            overflow: TextOverflow.ellipsis,
          ),
        ),
        Btn(
          label: 'Обновить',
          small: true,
          kind: BtnKind.primary,
          loading: updating,
          onPressed: state.updatingCore.isNotEmpty ? null : () => state.updateCore(update.kind),
        ),
      ],
    );
  }
}

class _PriorityCard extends StatelessWidget {
  final AppState state;
  const _PriorityCard({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final prio = state.setting<List>('cores.priority', const []).cast<String>();
    final off = allCores.where((k) => !prio.contains(k)).toList();

    void save(List<String> next) => state.updateSettings((s) => s['cores']['priority'] = next);

    Widget item(String k, int? index) {
      final s = coreStyle(k);
      final on = index != null;
      return Container(
        margin: const EdgeInsets.only(bottom: 8),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        decoration: BoxDecoration(
          color: p.surface2,
          borderRadius: BorderRadius.circular(11),
          border: Border.all(color: p.border),
        ),
        child: Row(
          children: [
            SizedBox(
              width: 16,
              child: Text(
                on ? '${index + 1}' : '–',
                style: TextStyle(fontFamily: monoFont, color: p.dim),
              ),
            ),
            const SizedBox(width: 8),
            CoreLogo(k, size: 28, off: !on),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    s.name,
                    style: TextStyle(fontWeight: FontWeight.w600, color: on ? p.text : p.muted),
                  ),
                  if (!state.info.installed(k)) Text('не установлено', style: TextStyle(fontSize: 11, color: p.dim)),
                ],
              ),
            ),
            if (on) ...[
              _Arrow(
                icon: Icons.keyboard_arrow_up,
                onTap: index > 0
                    ? () => save(
                        [...prio]
                          ..removeAt(index)
                          ..insert(index - 1, k),
                      )
                    : null,
              ),
              const SizedBox(width: 4),
              _Arrow(
                icon: Icons.keyboard_arrow_down,
                onTap: index < prio.length - 1
                    ? () => save(
                        [...prio]
                          ..removeAt(index)
                          ..insert(index + 1, k),
                      )
                    : null,
              ),
              const SizedBox(width: 8),
            ],
            Tooltip(
              message: on ? (prio.length == 1 ? 'Нужно хотя бы одно ядро' : 'Не использовать это ядро') : 'Использовать',
              child: Transform.scale(
                scale: .8,
                child: Switch(value: on, onChanged: on && prio.length == 1 ? null : (v) => save(v ? [...prio, k] : prio.where((x) => x != k).toList())),
              ),
            ),
          ],
        ),
      );
    }

    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Приоритет ядер', sub: 'сверху — предпочтительное'),
          for (final (i, k) in prio.indexed) item(k, i),
          for (final k in off) item(k, null),
          const SizedBox(height: 4),
          Text(
            'При подключении берётся первое ядро из списка, которое поддерживает протокол сервера. '
            'Несовместимые пропускаются, остальные становятся резервом.',
            style: TextStyle(fontSize: 12, color: p.dim, height: 1.5),
          ),
        ],
      ),
    );
  }
}

class _Arrow extends StatelessWidget {
  final IconData icon;
  final VoidCallback? onTap;
  const _Arrow({required this.icon, this.onTap});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    return Opacity(
      opacity: onTap == null ? .3 : 1,
      child: Material(
        color: p.surface,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(7),
          side: BorderSide(color: p.border),
        ),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: SizedBox(width: 28, height: 28, child: Icon(icon, size: 18, color: p.muted)),
        ),
      ),
    );
  }
}

class _RulesCard extends StatelessWidget {
  final AppState state;
  const _RulesCard({required this.state});

  Future<String?> _int(String key, String v) => state.updateSettings((s) => s['cores'][key] = int.tryParse(v) ?? 0);

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final interval = state.setting('cores.health_interval_s', 15);
    final failures = state.setting('cores.health_failures', 3);
    final maxLatency = state.setting('cores.max_latency_ms', 0);
    final returnAfter = state.setting('cores.return_after_min', 10);
    final small = TextStyle(fontSize: 12, color: p.muted);

    Widget inline(String key, int value) => Padding(
      padding: const EdgeInsets.symmetric(horizontal: 6),
      child: SavingField(value: '$value', width: 58, numeric: true, align: TextAlign.center, onSave: (v) => _int(key, v)),
    );

    Widget always(String title, String desc, {bool first = false}) => SettingRow(
      first: first,
      title: title,
      description: desc,
      trailing: Text('всегда', style: TextStyle(fontSize: 12, color: p.dim)),
    );

    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Правила переключения'),
          const SectionLabel('Триггеры'),
          always('Процесс ядра упал', 'Ненулевой код выхода или зависание процесса', first: true),
          always('Ядро отвергло конфиг', 'Проверка конфига не прошла — сразу берётся следующее ядро'),
          always('Протокол не поддерживается', 'Несовместимые ядра пропускаются при подключении'),
          SettingRow(
            title: 'Проверка связи не прошла',
            descriptionWidget: Wrap(
              crossAxisAlignment: WrapCrossAlignment.center,
              runSpacing: 6,
              children: [
                Text('после', style: small),
                inline('health_failures', failures),
                Text('неудач подряд, интервал', style: small),
                inline('health_interval_s', interval),
                Text('с', style: small),
              ],
            ),
            trailing: Text('всегда', style: TextStyle(fontSize: 12, color: p.dim)),
          ),
          SettingRow(
            title: 'Высокая задержка',
            descriptionWidget: maxLatency > 0
                ? Wrap(
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: [
                      Text('считать сбоем ответ дольше', style: small),
                      inline('max_latency_ms', maxLatency),
                      Text('мс', style: small),
                    ],
                  )
                : Text('Медленные ответы не считаются сбоем', style: small),
            trailing: Switch(value: maxLatency > 0, onChanged: (v) => state.updateSettings((s) => s['cores']['max_latency_ms'] = v ? 800 : 0)),
          ),
          const SizedBox(height: 6),
          const SectionLabel('Поведение'),
          SettingRow(
            first: true,
            title: 'Возвращаться к приоритетному',
            descriptionWidget: returnAfter > 0
                ? Wrap(
                    crossAxisAlignment: WrapCrossAlignment.center,
                    children: [
                      Text('пробовать основное ядро через', style: small),
                      inline('return_after_min', returnAfter),
                      Text('мин работы на резерве', style: small),
                    ],
                  )
                : Text('Оставаться на резервном ядре до переподключения', style: small),
            trailing: Switch(value: returnAfter > 0, onChanged: (v) => state.updateSettings((s) => s['cores']['return_after_min'] = v ? 10 : 0)),
          ),
          SettingRow(
            title: 'Адрес проверки связи',
            description: 'Должен отвечать 204 или 200',
            trailing: SavingField(
              value: state.setting('cores.health_url', ''),
              width: 250,
              mono: true,
              onSave: (v) => state.updateSettings((s) => s['cores']['health_url'] = v.trim()),
            ),
          ),
        ],
      ),
    );
  }
}

class _Matrix extends StatelessWidget {
  final DaemonInfo info;
  const _Matrix({required this.info});

  static const rows = [
    ('VLESS', 'protocol:vless'),
    ('VMess', 'protocol:vmess'),
    ('Trojan', 'protocol:trojan'),
    ('Shadowsocks', 'protocol:shadowsocks'),
    ('Hysteria2', 'protocol:hysteria2'),
    ('TUIC v5', 'protocol:tuic'),
    ('AnyTLS', 'protocol:anytls'),
    ('WireGuard', 'protocol:wireguard'),
    ('REALITY', 'reality'),
    ('WebSocket', 'transport:ws'),
    ('gRPC', 'transport:grpc'),
    ('HTTPUpgrade', 'transport:httpupgrade'),
    ('HTTP/2', 'transport:http'),
    ('XHTTP', 'transport:xhttp'),
  ];

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final head = TextStyle(fontSize: 11, color: p.dim, letterSpacing: .6, fontWeight: FontWeight.w600);
    final cores = [for (final k in allCores) info.cores.where((c) => c.kind == k).firstOrNull ?? CoreInfo(kind: k, installed: false, features: const [])];
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          const PanelTitle('Совместимость', sub: 'что умеет каждое ядро в CoreShift'),
          Table(
            columnWidths: const {0: FlexColumnWidth(1.6)},
            defaultVerticalAlignment: TableCellVerticalAlignment.middle,
            children: [
              TableRow(
                children: [
                  Padding(
                    padding: const EdgeInsets.all(9),
                    child: Text('ПРОТОКОЛ / ТРАНСПОРТ', style: head),
                  ),
                  for (final c in cores)
                    Padding(
                      padding: const EdgeInsets.all(9),
                      child: Center(
                        child: Text(coreStyle(c.kind).name.toUpperCase(), style: head.copyWith(color: coreStyle(c.kind).color)),
                      ),
                    ),
                ],
              ),
              for (final (label, f) in rows)
                TableRow(
                  decoration: BoxDecoration(
                    border: Border(top: BorderSide(color: p.border)),
                  ),
                  children: [
                    Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 9, vertical: 8),
                      child: Text(label, style: const TextStyle(fontSize: 13)),
                    ),
                    for (final c in cores)
                      Center(
                        child: c.features.contains(f) ? const Pill('да', color: okColor) : Text('—', style: TextStyle(color: p.dim, fontSize: 12)),
                      ),
                  ],
                ),
            ],
          ),
        ],
      ),
    );
  }
}
