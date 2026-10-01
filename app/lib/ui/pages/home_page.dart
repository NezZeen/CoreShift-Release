import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../api/models.dart';
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../shell.dart';
import 'servers_page.dart' show showAddSubscription;
import '../countries.dart';
import '../theme.dart';
import '../widgets.dart';

part 'home/hero.dart';
part 'home/compact.dart';
part 'home/connect.dart';
part 'home/cards.dart';

class HomePage extends StatelessWidget {
  final AppState state;
  const HomePage({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    if (state.subscriptions.isEmpty) return PageFrame(children: [_Welcome(state: state)]);
    if (isCompact(context)) {
      // Room above for the page to sit mid-screen when connected, the
      // tallest it gets, so the button stays put as the numbers come in.
      return LayoutBuilder(
        builder: (context, c) => PageFrame(
          children: [
            SizedBox(height: max(0, (c.maxHeight - 560) / 2)),
            _CompactHome(state: state),
          ],
        ),
      );
    }
    // The desktop: the connection, the address sites see, and while
    // connected its speed. The cores are on their page, the subscription on
    // its card.
    return LayoutBuilder(
      builder: (context, c) => PageFrame(
        children: [
          SizedBox(height: max(0, (c.maxHeight - 780) / 2)),
          Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 600),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  _Hero(state: state),
                  if (!state.ipUnsupported) ...[const SizedBox(height: 14), _IpCard(state: state)],
                  // Without a connection it has nothing to show.
                  if (state.status.active) ...[const SizedBox(height: 14), _SpeedCard(state: state)],
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}
