import 'package:flutter/material.dart';

import '../../api/models.dart';
import '../../state/app_state.dart';
import 'wizard_strings.dart';

/// A way of connecting the wizard offers. It is a row of the list in
/// [wizardModes]: a new mode (a system proxy, a proxy without VPN on
/// Android) is one more entry there, nothing else changes.
class WizardMode {
  final String id;
  final String title;
  final String text;
  final IconData icon;

  /// The one marked «Рекомендуем» (and chosen first, when it is available).
  final bool recommended;

  /// Whether this system can do it; [unavailable] says why when it cannot.
  final bool Function(AppState state) available;
  final String Function(AppState state) unavailable;

  /// Whether the settings already are this way.
  final bool Function(AppState state) isActive;

  /// Puts the mode into a copy of the settings, which is then saved.
  final void Function(Json settings) apply;

  const WizardMode({
    required this.id,
    required this.title,
    required this.text,
    required this.icon,
    this.recommended = false,
    this.available = _always,
    this.unavailable = _nothing,
    required this.isActive,
    required this.apply,
  });

  static bool _always(AppState _) => true;
  static String _nothing(AppState _) => '';
}

/// The modes of this system, in the order the wizard lists them. Only the
/// existing settings are used: the "tun" switch.
List<WizardMode> wizardModes({required bool android}) {
  if (android) {
    return [
      WizardMode(
        id: 'vpn',
        title: WizardStrings.modeVpnTitle,
        text: WizardStrings.modeVpnText,
        icon: Icons.vpn_key_outlined,
        recommended: true,
        isActive: (s) => s.setting('tun', true),
        apply: (j) => j['tun'] = true,
      ),
    ];
  }
  return [
    WizardMode(
      id: 'tun',
      title: WizardStrings.modeAllTitle,
      text: WizardStrings.modeAllText,
      icon: Icons.devices_outlined,
      recommended: true,
      available: (s) => s.info.tunAvailable,
      unavailable: (s) => s.info.tunUnavailable,
      isActive: (s) => s.setting('tun', false),
      apply: (j) => j['tun'] = true,
    ),
    WizardMode(
      id: 'proxy',
      title: WizardStrings.modeProxyTitle,
      text: WizardStrings.modeProxyText,
      icon: Icons.lan_outlined,
      isActive: (s) => !s.setting('tun', false),
      apply: (j) => j['tun'] = false,
    ),
  ];
}

/// The mode chosen when the step opens: the recommended one if this system
/// can do it, else the first that it can.
WizardMode? wizardDefaultMode(List<WizardMode> modes, AppState s) {
  final ok = modes.where((m) => m.available(s)).toList();
  return ok.where((m) => m.recommended).firstOrNull ?? ok.firstOrNull;
}
