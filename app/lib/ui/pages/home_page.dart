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
part 'home/quick_pick.dart';
part 'home/traffic.dart';

class HomePage extends StatelessWidget {
  final AppState state;
  const HomePage({super.key, required this.state});

  @override
  Widget build(BuildContext context) {
    if (state.subscriptions.isEmpty) return PageFrame(children: [_Welcome(state: state)]);
    // The traffic card shows once the history is loaded, so loading it
    // starts here.
    WidgetsBinding.instance.addPostFrameCallback((_) => state.watchStats());
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
    // The desktop: the connection, the address sites see, while connected
    // its speed, and the traffic of the last days. The cores are on their
    // page, the subscription on its card. A wide window puts the connection
    // beside the rest instead of above it.
    return LayoutBuilder(
      builder: (context, c) {
        final wide = c.maxWidth >= 900;
        final cards = [
          if (!state.ipUnsupported) _IpCard(state: state),
          // Without a connection it has nothing to show.
          if (state.status.active) _SpeedCard(state: state),
          if (!state.statsUnsupported && state.statsLoaded) _TrafficCard(state: state),
        ];
        final column = Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (final (i, card) in cards.indexed) ...[if (i > 0) const SizedBox(height: 14), card],
          ],
        );
        return PageFrame(
          children: [
            SizedBox(height: max(0, (c.maxHeight - (wide ? 600 : 780)) / 2)),
            Center(
              child: ConstrainedBox(
                constraints: BoxConstraints(maxWidth: wide ? 1060 : 600),
                child: wide
                    ? Row(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Expanded(flex: 5, child: _Hero(state: state)),
                          const SizedBox(width: 16),
                          Expanded(flex: 6, child: column),
                        ],
                      )
                    : Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          _Hero(state: state),
                          if (cards.isNotEmpty) const SizedBox(height: 14),
                          column,
                        ],
                      ),
              ),
            ),
          ],
        );
      },
    );
  }
}
