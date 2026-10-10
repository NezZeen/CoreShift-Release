// «Системный прокси»: the app, running as the user, has the daemon's binary
// set the proxy of the system and put it back (engine/internal/sysproxy).
export 'system_proxy_web.dart' if (dart.library.io) 'system_proxy_io.dart';
