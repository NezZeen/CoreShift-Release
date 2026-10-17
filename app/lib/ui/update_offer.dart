import 'package:flutter/material.dart';

import '../l10n/strings.dart';
import '../platform/platform.dart' as platform;
import '../state/app_state.dart';
import 'theme.dart';
import 'widgets.dart';

/// Offers a downloaded update of CoreShift: "Обновить" installs it (on
/// Android through the system's installer, which asks again), "Позже"
/// leaves it in the settings until the next start.
Future<void> showUpdateOffer(BuildContext context, AppState state) {
  final u = state.appUpdate;
  if (u.state == 'available') return _showDownloadOffer(context, state);
  final how = platform.isAndroid
      ? tr('update.how.android')
      : state.status.active
      ? tr('update.how.connected')
      : tr('update.how.desktop');
  return showDialog<void>(
    context: context,
    builder: (context) => Dialog(
      child: SizedBox(
        width: 420,
        child: Padding(
          padding: const EdgeInsets.all(22),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Icon(Icons.system_update_alt, color: okColor),
                  const SizedBox(width: 10),
                  Expanded(child: Text(tr('update.title'), style: dialogTitle)),
                ],
              ),
              const SizedBox(height: 12),
              Text(tr('update.ready', {'version': u.label})),
              if (u.notes.isNotEmpty) ...[const SizedBox(height: 8), Text(u.notes, style: TextStyle(color: context.pal.muted))],
              const SizedBox(height: 8),
              Text(how, style: TextStyle(color: context.pal.muted, fontSize: 13)),
              const SizedBox(height: 20),
              Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  Btn(label: tr('update.later'), onPressed: () => Navigator.pop(context)),
                  const SizedBox(width: 8),
                  Btn(
                    label: tr('update.install'),
                    icon: Icons.system_update_alt,
                    kind: BtnKind.primary,
                    onPressed: () {
                      Navigator.pop(context);
                      state.installAppUpdate();
                    },
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    ),
  );
}

/// Announces a new version where the system's packages install it (Linux):
/// «Скачать» opens its release page in the browser; nothing is installed
/// by CoreShift itself.
Future<void> _showDownloadOffer(BuildContext context, AppState state) {
  final u = state.appUpdate;
  return showDialog<void>(
    context: context,
    builder: (context) => Dialog(
      child: SizedBox(
        width: 420,
        child: Padding(
          padding: const EdgeInsets.all(22),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  const Icon(Icons.system_update_alt, color: okColor),
                  const SizedBox(width: 10),
                  Expanded(child: Text(tr('update.new_version', {'version': u.label}), style: dialogTitle)),
                ],
              ),
              const SizedBox(height: 12),
              if (u.notes.isNotEmpty) ...[Text(u.notes, style: TextStyle(color: context.pal.muted)), const SizedBox(height: 8)],
              Text(appUpdateDownloadHint, style: TextStyle(color: context.pal.muted, fontSize: 13)),
              const SizedBox(height: 20),
              Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  Btn(label: tr('update.later'), onPressed: () => Navigator.pop(context)),
                  const SizedBox(width: 8),
                  Btn(
                    label: tr('update.download'),
                    icon: Icons.open_in_new,
                    kind: BtnKind.primary,
                    onPressed: () {
                      Navigator.pop(context);
                      state.openUpdatePage();
                    },
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    ),
  );
}

/// How an announced version is installed (Linux).
String get appUpdateDownloadHint => tr('update.download_hint');
