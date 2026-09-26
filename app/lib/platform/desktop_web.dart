import 'package:flutter/widgets.dart';

import '../state/app_state.dart';

/// Nothing to set up in a browser.
Future<bool> initWindow({bool hidden = false}) async => false;

/// In a browser the page is the frame.
class DesktopFrame extends StatelessWidget {
  final AppState state;
  final Widget child;
  const DesktopFrame({super.key, required this.state, required this.child});

  @override
  Widget build(BuildContext context) => child;
}

/// A browser shows no CoreShift notifications.
bool get canNotify => false;
