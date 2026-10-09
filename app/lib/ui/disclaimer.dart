import 'package:flutter/material.dart';

import '../platform/platform.dart' as platform;
import '../state/app_state.dart';
import 'theme.dart';
import 'widgets.dart';

/// The pref that records the user's yes to [disclaimerPoints].
const disclaimerPref = 'disclaimer_accepted';

/// What CoreShift is and is not answerable for: shown once, before the app
/// is used, and again from the settings.
const disclaimerPoints = [
  (
    Icons.code,
    'CoreShift — бесплатная программа с открытым исходным кодом. Она не продаётся, '
        'и использовать её можно только в некоммерческих целях: для себя, для учёбы и исследований.',
  ),
  (
    Icons.dns_outlined,
    'CoreShift не предоставляет серверы, подписки и VPN-услуги и не связан с теми, кто их продаёт. '
        'За работу серверов, их содержимое и условия отвечает ваш провайдер.',
  ),
  (Icons.gavel_outlined, 'Вы сами решаете, как пользоваться программой, и сами отвечаете за соблюдение законов своей страны.'),
  (
    Icons.shield_outlined,
    'Программа распространяется «как есть», без каких-либо гарантий. '
        'Автор не отвечает за ущерб, потерю данных или иные последствия её использования.',
  ),
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
  if (state.status.active) await state.disconnect(from: 'отказ от условий');
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
                      Expanded(child: Text('Отказ от ответственности', style: dialogTitle)),
                    ],
                  ),
                  const SizedBox(height: 14),
                  for (final (icon, text) in disclaimerPoints)
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
                          Expanded(child: Text(text, style: const TextStyle(height: 1.45))),
                        ],
                      ),
                    ),
                  if (ask)
                    Text(
                      'Нажимая «Принимаю», вы соглашаетесь с этими условиями. Прочитать их снова можно в настройках.',
                      style: TextStyle(fontSize: 12.5, color: p.muted, height: 1.4),
                    ),
                  const SizedBox(height: 18),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: ask
                        ? [
                            Btn(label: 'Выйти', onPressed: () => Navigator.pop(context, false)),
                            const SizedBox(width: 8),
                            Btn(label: 'Принимаю', icon: Icons.check, kind: BtnKind.primary, onPressed: () => Navigator.pop(context, true)),
                          ]
                        : [Btn(label: 'Закрыть', onPressed: () => Navigator.pop(context))],
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
