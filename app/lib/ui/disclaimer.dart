import 'package:flutter/material.dart';

import '../l10n/strings.dart';
import '../platform/platform.dart' as platform;
import '../state/app_state.dart';
import 'theme.dart';
import 'widgets.dart';

/// The pref that records the user's yes to [disclaimerPoints].
const disclaimerPref = 'disclaimer_accepted';

/// What CoreShift is and is not answerable for: shown once, before the app
/// is used, and again from the settings. Each point is an icon and the key of
/// its text, said with [tr] where shown.
const disclaimerPoints = [
  (Icons.code, 'disclaimer.point.free'),
  (Icons.dns_outlined, 'disclaimer.point.no_service'),
  (Icons.gavel_outlined, 'disclaimer.point.law'),
  (Icons.shield_outlined, 'disclaimer.point.as_is'),
];

/// Shows the disclaimer once, until it is accepted. Declined, CoreShift
/// closes, and the VPN with it if it runs.
Future<void> offerDisclaimer(BuildContext context, AppState state) async {
  if (state.prefs[disclaimerPref] == true) return;
  final yes = await showDisclaimer(context, ask: true);
  if (yes == true) {
    state.setPref(disclaimerPref, true);
    return;
  }
  if (state.status.active) await state.disconnect(from: tr('disclaimer.from.decline'));
  await platform.quitApp();
}

/// The disclaimer in a window: with "Выйти" and "Принимаю" when [ask], with
/// "Закрыть" alone from the settings. Returns whether it was accepted.
Future<bool?> showDisclaimer(BuildContext context, {bool ask = false}) {
  return showDialog<bool>(
    context: context,
    barrierDismissible: !ask,
    builder: (context) {
      final p = context.pal;
      return PopScope(
        canPop: !ask,
        child: Dialog(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 480),
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(22),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Icon(Icons.info_outline, color: p.accentInk),
                      const SizedBox(width: 10),
                      Expanded(child: Text(tr('disclaimer.title'), style: dialogTitle)),
                    ],
                  ),
                  const SizedBox(height: 14),
                  for (final (icon, key) in disclaimerPoints)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 12),
                      child: Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Padding(
                            padding: const EdgeInsets.only(top: 1),
                            child: Icon(icon, size: 18, color: p.muted),
                          ),
                          const SizedBox(width: 12),
                          Expanded(child: Text(tr(key), style: const TextStyle(height: 1.45))),
                        ],
                      ),
                    ),
                  if (ask) Text(tr('disclaimer.accept_note'), style: TextStyle(fontSize: 12.5, color: p.muted, height: 1.4)),
                  const SizedBox(height: 18),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: ask
                        ? [
                            Btn(label: tr('disclaimer.quit'), onPressed: () => Navigator.pop(context, false)),
                            const SizedBox(width: 8),
                            Btn(label: tr('disclaimer.accept'), icon: Icons.check, kind: BtnKind.primary, onPressed: () => Navigator.pop(context, true)),
                          ]
                        : [Btn(label: tr('disclaimer.close'), onPressed: () => Navigator.pop(context))],
                  ),
                ],
              ),
            ),
          ),
        ),
      );
    },
  );
}
