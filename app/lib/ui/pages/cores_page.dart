import 'package:flutter/material.dart';

import '../../api/models.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../shell.dart';
import '../theme.dart';
import '../widgets.dart';

class CoresPage extends StatelessWidget {
  final AppState state;
  const CoresPage({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    final mode = state.setting('cores.mode', 'auto');
    final header = PageHeader(
      'Ядра',
      subtitle: 'Установленные ядра, их приоритет и правила автоматического переключения.',
      back: ('Настройки', () => Nav.to(context, PageId.settings)),
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
    );
    return PageFrame(
      children: [
        // The list is what matters; how the switching decides is folded away.
        Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 780),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                header,
                _Backup(state: state),
                _CoreList(state: state),
                const SizedBox(height: 16),
                Fold(
                  title: 'Правила переключения',
                  sub: 'когда сменить ядро и когда вернуться',
                  child: _RulesCard(state: state),
                ),
                const SizedBox(height: 16),
                Fold(
                  title: 'Совместимость',
                  sub: 'что умеет каждое ядро в CoreShift',
                  child: _Matrix(info: state.info),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

/// Under a core once updates were checked: up to date, a newer
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
        Icon(Icons.new_releases_outlined, size: 15, color: p.accentInk),
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

/// Every core in one list: its version and state, its update, and in the
/// automatic mode its place in the queue; in the manual one the choice.
class _CoreList extends StatelessWidget {
  final AppState state;
  const _CoreList({required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final st = state.status;
    final manual = state.setting('cores.mode', 'auto') == 'manual';
    final picked = state.setting('cores.manual', '');
    final prio = state.setting<List>('cores.priority', const []).cast<String>();
    final off = allCores.where((k) => !prio.contains(k)).toList();

    void save(List<String> next) => state.updateSettings((s) => s['cores']['priority'] = next);

    Widget item(String k, int? index) {
      final s = coreStyle(k);
      final installed = state.info.installed(k);
      final on = manual ? picked == k : index != null;
      final version = state.info.versionOf(k);
      final update = state.updateOf(k);
      final (pill, pillColor) = st.active && st.core == k
          ? ('работает', okColor)
          : st.active && st.failed.containsKey(k)
          ? ('сбой', errColor)
          : ('', p.dim);
      final row = Row(
        children: [
          SizedBox(
            width: 22,
            child: manual
                ? Icon(on ? Icons.radio_button_checked : Icons.radio_button_off, size: 17, color: on ? accent : p.dim)
                : Text(on ? '${index! + 1}' : '–', style: figures(15, color: p.dim)),
          ),
          const SizedBox(width: 6),
          CoreLogo(k, size: 30, off: !on || !installed),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    Flexible(
                      child: Text(
                        s.name,
                        style: TextStyle(fontWeight: FontWeight.w600, color: on ? p.text : p.muted),
                      ),
                    ),
                    if (pill.isNotEmpty) ...[const SizedBox(width: 8), Pill(pill, color: pillColor)],
                  ],
                ),
                Text(
                  !installed ? (platform.isAndroid ? 'нет в этой сборке' : 'нет в папке ядер') : (version.isEmpty ? 'установлено' : 'версия $version'),
                  style: TextStyle(fontSize: 11.5, color: p.dim, fontFamily: version.isEmpty || !installed ? null : monoFont),
                ),
              ],
            ),
          ),
          if (!manual && on) ...[
            _Arrow(
              icon: Icons.keyboard_arrow_up,
              onTap: index! > 0
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
          if (!manual)
            Tooltip(
              message: on ? (prio.length == 1 ? 'Нужно хотя бы одно ядро' : 'Не использовать это ядро') : 'Использовать',
              child: Transform.scale(
                scale: .8,
                child: Switch(value: on, onChanged: on && prio.length == 1 ? null : (v) => save(v ? [...prio, k] : prio.where((x) => x != k).toList())),
              ),
            ),
        ],
      );
      return Container(
        margin: const EdgeInsets.only(bottom: 8),
        decoration: BoxDecoration(
          color: manual && on ? accent.withValues(alpha: .08) : p.surface2,
          borderRadius: BorderRadius.circular(11),
          border: Border.all(color: manual && on ? accent.withValues(alpha: .6) : p.border),
        ),
        child: Material(
          color: Colors.transparent,
          child: InkWell(
            borderRadius: BorderRadius.circular(11),
            // The manual mode: a tap picks the core.
            onTap: manual && installed && !on ? () => state.updateSettings((x) => x['cores']['manual'] = k) : null,
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  row,
                  if (installed && update != null)
                    Padding(
                      padding: const EdgeInsets.only(left: 28, top: 8),
                      child: _UpdateRow(state: state, update: update, updating: state.updatingCore == k),
                    ),
                ],
              ),
            ),
          ),
        ),
      );
    }

    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle('Ядра', sub: manual ? 'работает только выбранное' : 'сверху — предпочтительное'),
          if (manual)
            for (final k in [...prio, ...off]) item(k, null)
          else ...[for (final (i, k) in prio.indexed) item(k, i), for (final k in off) item(k, null)],
          const SizedBox(height: 4),
          Text(
            manual
                ? 'Ручной режим: нажмите на ядро, чтобы выбрать его. Переключения при сбоях не будет.'
                : 'При подключении берётся первое ядро из списка, которое поддерживает протокол сервера. '
                      'Несовместимые пропускаются, остальные становятся резервом.',
            style: TextStyle(fontSize: 12, color: p.dim, height: 1.5),
          ),
        ],
      ),
    );
  }
}

/// While connected on a backup core: which one failed, and the way back
/// without waiting for the timer.
class _Backup extends StatelessWidget {
  final AppState state;
  const _Backup({required this.state});

  @override
  Widget build(BuildContext context) {
    final st = state.status;
    final chain = st.chain;
    final manual = state.setting('cores.mode', 'auto') == 'manual';
    if (st.state != ConnState.connected || manual || chain.length < 2 || st.core.isEmpty || st.core == chain.first) return const SizedBox();
    final primary = coreStyle(chain.first).name;
    final why = st.failed[chain.first] ?? '';
    return Padding(
      padding: const EdgeInsets.only(bottom: 18),
      child: Panel(
        borderColor: swapColor.withValues(alpha: .5),
        padding: const EdgeInsets.fromLTRB(16, 14, 14, 14),
        child: Row(
          children: [
            const Icon(Icons.swap_horiz, color: swapColor),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('Работает резервное ядро: ${coreStyle(st.core).name}', style: const TextStyle(fontWeight: FontWeight.w600)),
                  const SizedBox(height: 2),
                  Text(
                    '$primary перестало работать${why.isEmpty ? '' : ': ${humanError(why)}'}. Вернуть его можно сразу: сначала оно проверяется в фоне.',
                    style: TextStyle(fontSize: 12, color: context.pal.muted),
                  ),
                ],
              ),
            ),
            const SizedBox(width: 12),
            Btn(label: 'Вернуть $primary', icon: Icons.undo, small: true, loading: state.returning, onPressed: state.busy ? null : state.returnToPrimary),
          ],
        ),
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

    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
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
            description: 'Отвечает 204 или 200. Запасные: Google, Apple',
            trailing: SavingField(
              value: state.setting('cores.health_url', ''),
              width: 330,
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
    final head = TextStyle(fontSize: 12, color: p.dim, fontWeight: FontWeight.w600);
    final cores = [for (final k in allCores) info.cores.where((c) => c.kind == k).firstOrNull ?? CoreInfo(kind: k, installed: false, features: const [])];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Table(
          columnWidths: const {0: FlexColumnWidth(1.6)},
          defaultVerticalAlignment: TableCellVerticalAlignment.middle,
          children: [
            TableRow(
              children: [
                Padding(
                  padding: const EdgeInsets.all(9),
                  child: Text('Протокол или транспорт', style: head),
                ),
                for (final c in cores)
                  Padding(
                    padding: const EdgeInsets.all(9),
                    child: Center(
                      child: Text(coreStyle(c.kind).name, style: head.copyWith(color: coreStyle(c.kind).color)),
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
    );
  }
}
