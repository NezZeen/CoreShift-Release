import 'dart:async';
import 'dart:math';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../api/models.dart';
import '../../l10n/strings.dart';
import '../../platform/platform.dart' as platform;
import '../../state/announcements.dart';
import '../../state/app_state.dart';
import '../../state/errors.dart';
import '../announcement.dart';
import '../checkup.dart';
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
part 'home/speed_test.dart';
part 'home/direct_hint.dart';

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
            SizedBox(height: max(0, (c.maxHeight - 720) / 2)),
            _CompactHome(state: state),
          ],
        ),
      );
    }
    // The desktop: the button and its state on the left; on the right the
    // way the traffic takes (this device, the server, the address sites
    // see), while connected the speed, and quietly under it the speed test
    // and the traffic. The DNS leak test is in the settings, by its switch.
    return LayoutBuilder(
      builder: (context, c) {
        final wide = c.maxWidth >= 900;
        final st = state.status;
        final warning = state.subscriptionWarnings.isNotEmpty;
        final announced = state.announcements.length;
        final left = Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _Hero(state: state),
            if (state.vpnBlocked) ...[const SizedBox(height: 16), _VpnBlockedBanner(state: state)],
            if (st.state == ConnState.noNetwork) ...[const SizedBox(height: 16), _NoNetworkBanner(state: state)],
            if (state.serverUnresponsive) ...[const SizedBox(height: 16), _UnresponsiveBanner(state: state)],
            if (state.directHint) ...[const SizedBox(height: 16), _DirectHintBanner(state: state)],
            if (st.settingsPending) ...[const SizedBox(height: 16), _PendingBanner(state: state)],
            _BackupBanner(state: state),
          ],
        );
        final right = Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            _Route(state: state),
            // Without a connection it has nothing to show.
            if (st.active) ...[const SizedBox(height: 14), _SpeedCard(state: state)],
            // The speed test and the traffic, quiet, under the rest.
            const SizedBox(height: 14),
            _HomeTools(state: state),
          ],
        );
        return PageFrame(
          children: [
            SizedBox(height: max(0, (c.maxHeight - (wide ? 640 : 940) - (warning ? 70 : 0) - announced * 150) / 2)),
            Center(
              child: ConstrainedBox(
                constraints: BoxConstraints(maxWidth: wide ? 1040 : 600),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    AnnouncementCards(state: state, gap: 22),
                    if (warning) ...[_SubWarningBanner(state: state), const SizedBox(height: 22)],
                    wide
                        ? Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Expanded(
                                flex: 5,
                                child: Padding(padding: const EdgeInsets.only(top: 8), child: left),
                              ),
                              const SizedBox(width: 40),
                              Expanded(flex: 6, child: right),
                            ],
                          )
                        : Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [left, const SizedBox(height: 24), right]),
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
