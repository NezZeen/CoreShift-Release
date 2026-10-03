import 'package:flutter/material.dart';

import '../../api/models.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../theme.dart';
import '../widgets.dart';

/// A list of programs, by executable name: the ones that bypass the tunnel
/// (games, torrent clients, banking apps that refuse foreign addresses) or
/// the ones that always go through it.
class AppsPanel extends StatefulWidget {
  final AppState state;

  /// The list under "routing", e.g. "direct_apps".
  final String setting;
  final String title;
  final String sub;
  final String description;
  final String empty;

  /// Marks a program that is on the list in the picker.
  final String badge;
  final String removeTip;

  const AppsPanel({
    super.key,
    required this.state,
    required this.setting,
    required this.title,
    required this.sub,
    required this.description,
    required this.empty,
    required this.badge,
    required this.removeTip,
  });

  /// Programs that bypass the tunnel.
  const AppsPanel.direct({super.key, required this.state})
    : setting = 'direct_apps',
      title = 'Программы без VPN',
      sub = 'игры, торренты, банки',
      description =
          'Эти программы работают напрямую, как без VPN, где бы они ни были установлены. Удобно для игр (меньше пинг), '
          'торрентов и банковских приложений, которые не пускают из-за границы.',
      empty = 'Пока пусто — все программы идут через VPN',
      badge = 'без VPN',
      removeTip = 'Снова через VPN';

  /// Programs that always go through the tunnel.
  const AppsPanel.proxy({super.key, required this.state, required this.description})
    : setting = 'proxy_apps',
      title = 'Программы через VPN',
      sub = 'весь их трафик',
      empty = 'Пока пусто',
      badge = 'через VPN',
      removeTip = 'Убрать из списка';

  @override
  State<AppsPanel> createState() => _AppsPanelState();
}

class _AppsPanelState extends State<AppsPanel> {
  final _add = TextEditingController();
  String? _error;

  AppState get s => widget.state;

  List<String> get _apps => s.setting<List>('routing.${widget.setting}', const []).cast<String>();

  Future<void> _addNames(Iterable<String> names) async {
    final input = names.map((n) => n.trim()).where((n) => n.isNotEmpty).toList();
    if (input.isEmpty) return;
    final err = await s.updateSettings((x) => x['routing'][widget.setting] = [..._apps, ...input]);
    if (!mounted) return;
    setState(() => _error = err);
    if (err == null) _add.clear();
  }

  Future<void> _remove(String name) async {
    final err = await s.updateSettings((x) => x['routing'][widget.setting] = _apps.where((a) => a != name).toList());
    if (mounted) setState(() => _error = err);
  }

  @override
  void dispose() {
    _add.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final apps = _apps;
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle(widget.title, sub: widget.sub),
          Text(widget.description, style: TextStyle(fontSize: 12, color: p.muted, height: 1.45)),
          const SizedBox(height: 12),
          Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _add,
                  onSubmitted: (v) => _addNames([v]),
                  onChanged: (_) => setState(() => _error = null),
                  style: const TextStyle(fontSize: 13, fontFamily: monoFont, fontFamilyFallback: monoFallback),
                  // Linux executables have no extension.
                  decoration: InputDecoration(hintText: platform.isLinux ? 'steam' : 'steam.exe'),
                ),
              ),
              const SizedBox(width: 8),
              Btn(label: 'Добавить', icon: Icons.add, onPressed: () => _addNames([_add.text])),
            ],
          ),
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerLeft,
            child: Btn(label: 'Выбрать из запущенных', icon: Icons.apps, kind: BtnKind.ghost, small: true, onPressed: s.online ? () => _pick(context) : null),
          ),
          if (_error != null)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(_error!, style: const TextStyle(color: errColor, fontSize: 12)),
            ),
          const SizedBox(height: 12),
          if (apps.isEmpty)
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
                for (final a in apps)
                  Container(
                    padding: const EdgeInsets.fromLTRB(8, 4, 4, 4),
                    decoration: BoxDecoration(
                      color: p.surface2,
                      borderRadius: BorderRadius.circular(8),
                      border: Border.all(color: p.border),
                    ),
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Icon(Icons.web_asset, size: 14, color: p.muted),
                        const SizedBox(width: 6),
                        Text(a, style: const TextStyle(fontSize: 13)),
                        const SizedBox(width: 2),
                        Tooltip(
                          message: widget.removeTip,
                          child: InkWell(
                            borderRadius: BorderRadius.circular(6),
                            onTap: () => _remove(a),
                            child: Padding(
                              padding: const EdgeInsets.all(4),
                              child: Icon(Icons.close, size: 14, color: p.dim),
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
              ],
            ),
        ],
      ),
    );
  }

  Future<void> _pick(BuildContext context) async {
    await showDialog<void>(
      context: context,
      builder: (_) => _AppPicker(state: s, setting: widget.setting, badge: widget.badge, onAdd: (name) => _addNames([name])),
    );
  }
}

/// Lists the running programs; each can be added with one click.
class _AppPicker extends StatefulWidget {
  final AppState state;
  final String setting;
  final String badge;
  final Future<void> Function(String name) onAdd;
  const _AppPicker({required this.state, required this.setting, required this.badge, required this.onAdd});

  @override
  State<_AppPicker> createState() => _AppPickerState();
}

class _AppPickerState extends State<_AppPicker> {
  List<RunningApp>? _apps;
  String? _error;
  String _query = '';

  @override
  void initState() {
    super.initState();
    _load();
    widget.state.addListener(_changed);
  }

  @override
  void dispose() {
    widget.state.removeListener(_changed);
    super.dispose();
  }

  void _changed() => setState(() {});

  Future<void> _load() async {
    try {
      final apps = await widget.state.runningApps();
      if (mounted) setState(() => _apps = apps);
    } catch (e) {
      if (mounted) setState(() => _error = humanError('$e'));
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final chosen = widget.state.setting<List>('routing.${widget.setting}', const []).map((a) => '$a'.toLowerCase()).toSet();
    final q = _query.toLowerCase();
    final list = (_apps ?? const <RunningApp>[]).where((a) => q.isEmpty || a.name.toLowerCase().contains(q) || a.path.toLowerCase().contains(q)).toList();

    return Dialog(
      child: SizedBox(
        width: 560,
        height: 520,
        child: Padding(
          padding: const EdgeInsets.all(22),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text('Запущенные программы', style: dialogTitle),
              const SizedBox(height: 4),
              Text(
                'Запустите нужную программу, если её нет в списке. Системные программы ${platform.isLinux ? '' : 'Windows '}не показываются.',
                style: TextStyle(fontSize: 12, color: p.muted),
              ),
              const SizedBox(height: 12),
              TextField(
                autofocus: true,
                onChanged: (v) => setState(() => _query = v),
                style: const TextStyle(fontSize: 13),
                decoration: InputDecoration(
                  hintText: 'Поиск',
                  prefixIcon: Icon(Icons.search, size: 17, color: p.dim),
                ),
              ),
              const SizedBox(height: 10),
              Expanded(
                child: _error != null
                    ? Center(
                        child: Text(_error!, style: const TextStyle(color: errColor, fontSize: 12)),
                      )
                    : _apps == null
                    ? const Center(child: CircularProgressIndicator(strokeWidth: 2))
                    : list.isEmpty
                    ? Center(
                        child: Text('Ничего не нашлось', style: TextStyle(color: p.dim, fontSize: 12)),
                      )
                    : ListView.builder(
                        itemCount: list.length,
                        itemBuilder: (context, i) {
                          final a = list[i];
                          final added = chosen.contains(a.name.toLowerCase());
                          return Padding(
                            padding: const EdgeInsets.symmetric(vertical: 3),
                            child: Row(
                              children: [
                                Icon(Icons.web_asset, size: 18, color: p.muted),
                                const SizedBox(width: 10),
                                Expanded(
                                  child: Column(
                                    crossAxisAlignment: CrossAxisAlignment.start,
                                    children: [
                                      Text(a.name, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w500)),
                                      Text(
                                        a.path,
                                        style: TextStyle(fontSize: 11, color: p.dim),
                                        overflow: TextOverflow.ellipsis,
                                      ),
                                    ],
                                  ),
                                ),
                                const SizedBox(width: 8),
                                added
                                    ? Padding(
                                        padding: const EdgeInsets.symmetric(horizontal: 8),
                                        child: Text(widget.badge, style: TextStyle(fontSize: 12, color: context.pal.okInk)),
                                      )
                                    : Btn(label: 'Добавить', small: true, onPressed: () => widget.onAdd(a.name)),
                              ],
                            ),
                          );
                        },
                      ),
              ),
              const SizedBox(height: 12),
              Row(
                children: [
                  Btn(
                    label: 'Обновить список',
                    icon: Icons.refresh,
                    kind: BtnKind.ghost,
                    onPressed: () {
                      setState(() => _apps = null);
                      _load();
                    },
                  ),
                  const Spacer(),
                  Btn(label: 'Готово', kind: BtnKind.primary, onPressed: () => Navigator.pop(context)),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
