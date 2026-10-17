import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../l10n/strings.dart';
import '../../platform/platform.dart' as platform;
import '../../state/app_state.dart';
import '../theme.dart';
import '../widgets.dart';

/// Android's per-app VPN: which apps use it at all. Android applies it to
/// the VPN itself, so the rules below only see the apps inside.
class AndroidAppsPanel extends StatelessWidget {
  final AppState state;
  const AndroidAppsPanel({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final mode = state.setting('routing.app_filter', 'all');
    final apps = state.setting<List>('routing.filter_apps', const []).cast<String>();
    final choices = [
      ('all', tr('apps.android.all'), tr('apps.android.all.desc')),
      ('exclude', tr('apps.android.exclude'), tr('apps.android.exclude.desc')),
      ('only', tr('apps.android.only'), tr('apps.android.only.desc')),
    ];
    return Panel(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          PanelTitle(tr('apps.android.title')),
          for (final (value, title, text) in choices)
            InkWell(
              borderRadius: BorderRadius.circular(10),
              onTap: value == mode ? null : () => state.updateSettings((s) => s['routing']['app_filter'] = value),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 6),
                child: Row(
                  children: [
                    Icon(value == mode ? Icons.radio_button_checked : Icons.radio_button_unchecked, size: 20, color: value == mode ? accent : p.dim),
                    const SizedBox(width: 12),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(title, style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w500)),
                          Text(text, style: TextStyle(fontSize: 12, color: p.muted)),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
            ),
          if (mode != 'all') ...[
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: Text(
                    apps.isEmpty ? tr(mode == 'only' ? 'apps.android.none.only' : 'apps.android.none') : tr('apps.android.chosen', {'n': apps.length}),
                    style: TextStyle(fontSize: 13, color: apps.isEmpty && mode == 'only' ? p.warnInk : p.text),
                  ),
                ),
                Btn(
                  label: tr('apps.android.select'),
                  icon: Icons.apps,
                  small: true,
                  onPressed: () => Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      fullscreenDialog: true,
                      builder: (_) => _AppPicker(state: state, only: mode == 'only'),
                    ),
                  ),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

/// Icons load once per run: there may be hundreds.
final _icons = <String, Future<Uint8List?>>{};

Future<Uint8List?> _icon(String pkg) => _icons.putIfAbsent(pkg, () => platform.appIcon(pkg));

class _AppPicker extends StatefulWidget {
  final AppState state;
  final bool only;
  const _AppPicker({required this.state, required this.only});

  @override
  State<_AppPicker> createState() => _AppPickerState();
}

class _AppPickerState extends State<_AppPicker> {
  late final Set<String> chosen = widget.state.setting<List>('routing.filter_apps', const []).cast<String>().toSet();
  List<platform.AndroidApp>? apps;
  String query = '';
  bool system = false;
  bool saving = false;

  @override
  void initState() {
    super.initState();
    platform.installedApps().then((list) {
      if (!mounted) return;
      // The chosen first, as they were when the list opened; then by name.
      list.sort((a, b) {
        final c = (chosen.contains(a.package) ? 0 : 1).compareTo(chosen.contains(b.package) ? 0 : 1);
        return c != 0 ? c : a.label.toLowerCase().compareTo(b.label.toLowerCase());
      });
      setState(() => apps = list);
    });
  }

  Future<void> _save() async {
    setState(() => saving = true);
    final list = chosen.toList()..sort();
    final err = await widget.state.updateSettings((s) => s['routing']['filter_apps'] = list);
    if (!mounted) return;
    setState(() => saving = false);
    if (err == null) Navigator.of(context).pop();
  }

  @override
  Widget build(BuildContext context) {
    final p = context.pal;
    final all = apps;
    final shown = all
        ?.where((a) => system || !a.system || chosen.contains(a.package))
        .where((a) => query.isEmpty || a.label.toLowerCase().contains(query) || a.package.toLowerCase().contains(query))
        .toList();
    return Scaffold(
      appBar: AppBar(
        title: Text(tr(widget.only ? 'apps.android.picker.only' : 'apps.android.picker.exclude'), style: dialogTitle),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: Center(
              child: Btn(label: tr('apps.done'), kind: BtnKind.primary, small: true, loading: saving, onPressed: _save),
            ),
          ),
        ],
      ),
      body: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(14, 4, 14, 6),
            child: TextField(
              onChanged: (v) => setState(() => query = v.trim().toLowerCase()),
              decoration: InputDecoration(
                hintText: tr('apps.android.picker.search'),
                prefixIcon: Icon(Icons.search, size: 18, color: p.dim),
              ),
            ),
          ),
          SwitchListTile(
            dense: true,
            title: Text(tr('apps.android.picker.system'), style: const TextStyle(fontSize: 13)),
            subtitle: Text(tr('apps.android.chosen', {'n': chosen.length}), style: TextStyle(fontSize: 12, color: p.muted)),
            value: system,
            onChanged: (v) => setState(() => system = v),
          ),
          Expanded(
            child: shown == null
                ? const Center(child: CircularProgressIndicator())
                : shown.isEmpty
                ? Center(
                    child: Text(tr('apps.android.picker.empty'), style: TextStyle(color: p.dim)),
                  )
                : ListView.builder(
                    itemCount: shown.length,
                    itemBuilder: (context, i) {
                      final a = shown[i];
                      final on = chosen.contains(a.package);
                      return CheckboxListTile(
                        value: on,
                        onChanged: (v) => setState(() => v == true ? chosen.add(a.package) : chosen.remove(a.package)),
                        secondary: SizedBox(
                          width: 36,
                          height: 36,
                          child: FutureBuilder<Uint8List?>(
                            future: _icon(a.package),
                            builder: (context, snap) =>
                                snap.data == null ? Icon(Icons.android, color: p.dim) : Image.memory(snap.data!, width: 36, height: 36, gaplessPlayback: true),
                          ),
                        ),
                        title: Text(a.label, maxLines: 1, overflow: TextOverflow.ellipsis),
                        subtitle: Text(
                          a.package,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(fontSize: 11, color: p.dim),
                        ),
                      );
                    },
                  ),
          ),
        ],
      ),
    );
  }
}
