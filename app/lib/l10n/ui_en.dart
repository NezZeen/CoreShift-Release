/// The UI's texts in English (see strings.dart). A key missing here is
/// shown in Russian.
library;

import 'parts/errors.dart';
import 'parts/settings.dart';
import 'parts/routing.dart';
import 'parts/servers.dart';
import 'parts/home.dart';
import 'parts/events.dart';
import 'parts/state.dart';
import 'parts/shell.dart';

const uiEn = <String, String>{
  // The navigation (ui/shell.dart).
  'nav.home': 'Home',
  'nav.servers': 'Servers',
  'nav.routing': 'Rules',
  'nav.logs': 'Journal',
  'nav.settings': 'Settings',

  // The connection's state on the home page (ui/pages/home/hero.dart).
  'home.state.connected': 'Connected',
  'home.state.connecting': 'Connecting…',
  'home.state.disconnecting': 'Disconnecting…',
  'home.state.failed': 'Connection failed',
  'home.state.waiting': 'Waiting for network…',
  'home.state.no_network': 'No network',
  'home.state.idle': 'Disconnected',
  'home.connect': 'Connect',
  'home.disconnect': 'Disconnect',

  // The settings (ui/pages/settings_page.dart).
  'settings.language': 'Language',
  'settings.language.system': 'System',
  'settings.language.ru': 'Русский',
  'settings.language.en': 'English',

  // The areas (parts/).
  ...errorsEn,
  ...settingsEn,
  ...routingEn,
  ...serversEn,
  ...homeEn,
  ...eventsEn,
  ...stateEn,
  ...shellEn,
};
