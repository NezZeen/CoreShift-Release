// Types mirroring the daemon's JSON API (engine/internal/service/api*.go).

import '../version.dart';

typedef Json = Map<String, dynamic>;

DateTime? _time(dynamic v) {
  if (v is! String || v.isEmpty) return null;
  final t = DateTime.tryParse(v);
  // Go writes the zero time as 0001-01-01; treat it as absent.
  if (t == null || t.year < 1971) return null;
  return t.toLocal();
}

List<String> _strings(dynamic v) => v is List ? v.map((e) => '$e').toList() : const [];

/// [noNetwork]: the device has no network. A connection either waits for one
/// to start ([Status.waiting]) or is held as it is until it returns.
enum ConnState { idle, connecting, connected, disconnecting, failed, noNetwork }

ConnState _connState(dynamic v) => switch (v) {
  'connecting' => ConnState.connecting,
  'no-network' => ConnState.noNetwork,
  'connected' => ConnState.connected,
  'disconnecting' => ConnState.disconnecting,
  'failed' => ConnState.failed,
  _ => ConnState.idle,
};

class Status {
  final ConnState state;
  final String node;
  final String protocol;
  final String core;
  final List<String> chain;
  final Map<String, String> failed;
  final bool tun;
  final DateTime? since;
  final String error;
  final bool settingsPending;

  /// While connected and nothing gets through: who is at fault, as the
  /// service found looking past the tunnel: "server-down" (the internet
  /// answers, the server does not), "offline" (nothing answers),
  /// "server-up" (the server answers, the tunnel through it does not) or
  /// "unknown". Empty while the server answers.
  final String problem;

  /// With [ConnState.noNetwork]: nothing is up yet, the connection starts
  /// once there is a network. Without it the connection is held.
  final bool waiting;

  /// While connected: direct connections do not get through this network,
  /// though the tunnel works (the network lets only a white list through,
  /// or Russian sites are out of reach from here). What the settings send
  /// direct had better go through the VPN.
  final bool directBlocked;

  const Status({
    this.state = ConnState.idle,
    this.node = '',
    this.protocol = '',
    this.core = '',
    this.chain = const [],
    this.failed = const {},
    this.tun = false,
    this.since,
    this.error = '',
    this.settingsPending = false,
    this.problem = '',
    this.waiting = false,
    this.directBlocked = false,
  });

  factory Status.fromJson(Json j) => Status(
    state: _connState(j['state']),
    node: j['node'] ?? '',
    protocol: j['protocol'] ?? '',
    core: j['core'] ?? '',
    chain: _strings(j['chain']),
    failed: (j['failed'] as Map?)?.map((k, v) => MapEntry('$k', '$v')) ?? const {},
    tun: j['tun'] == true,
    since: _time(j['since']),
    error: j['error'] ?? '',
    settingsPending: j['settings_pending'] == true,
    problem: j['problem'] ?? '',
    waiting: j['waiting'] == true,
    directBlocked: j['direct_blocked'] == true,
  );

  /// On, coming up, or waiting for the network to do either: the button
  /// then disconnects (or cancels).
  bool get active => state == ConnState.connected || state == ConnState.connecting || state == ConnState.noNetwork;
}

class Event {
  final DateTime time;
  final String kind;
  final String state;
  final String core;
  final String from;
  final String reason;
  final String error;
  final int latencyMs;
  final String line;
  final String source;
  final bool probe;
  final String subscription;
  final String fingerprint;
  final String method;
  // Traffic events: bytes this connection, and per second.
  final int up;
  final int down;
  final int upRate;
  final int downRate;

  /// A "checkup" event's step and how it went ("ok", "warn", "fail",
  /// "skipped"); the verdict's status on "done".
  final String step;
  final String status;

  const Event({
    required this.time,
    required this.kind,
    this.state = '',
    this.core = '',
    this.from = '',
    this.reason = '',
    this.error = '',
    this.latencyMs = 0,
    this.line = '',
    this.source = '',
    this.probe = false,
    this.subscription = '',
    this.fingerprint = '',
    this.method = '',
    this.up = 0,
    this.down = 0,
    this.upRate = 0,
    this.downRate = 0,
    this.step = '',
    this.status = '',
  });

  factory Event.fromJson(Json j) => Event(
    time: _time(j['time']) ?? DateTime.now(),
    kind: j['kind'] ?? '',
    state: j['state'] ?? '',
    core: j['core'] ?? '',
    from: j['from'] ?? '',
    reason: j['reason'] ?? '',
    error: j['error'] ?? '',
    latencyMs: (j['latency_ms'] as num?)?.toInt() ?? 0,
    line: j['line'] ?? '',
    source: j['source'] ?? '',
    probe: j['probe'] == true,
    subscription: j['subscription'] ?? '',
    fingerprint: j['fingerprint'] ?? '',
    method: j['method'] ?? '',
    up: (j['up'] as num?)?.toInt() ?? 0,
    down: (j['down'] as num?)?.toInt() ?? 0,
    upRate: (j['up_rate'] as num?)?.toInt() ?? 0,
    downRate: (j['down_rate'] as num?)?.toInt() ?? 0,
    step: j['step'] ?? '',
    status: j['status'] ?? '',
  );
}

/// A node's last latency test: [ms] when it worked, else [error]. [method]
/// is how it was measured: "icmp", "tcp" or "proxy" (through [core]).
class Latency {
  final int ms;
  final String error;
  final String core;
  final String method;
  const Latency({this.ms = 0, this.error = '', this.core = '', this.method = ''});

  bool get ok => error.isEmpty && ms > 0;
}

class NodeView {
  final String fingerprint;
  final String name;
  final String protocol;
  final String transport;
  final String security;
  final String server;
  final int port;
  final List<String> cores;
  final Latency? latency;

  /// The user removed the server from the list (Subscription.hiddenNodes).
  final bool hidden;

  const NodeView({
    required this.fingerprint,
    required this.name,
    required this.protocol,
    required this.transport,
    required this.security,
    required this.server,
    required this.port,
    required this.cores,
    this.latency,
    this.hidden = false,
  });

  factory NodeView.fromJson(Json j) => NodeView(
    fingerprint: j['fingerprint'] ?? '',
    name: j['name'] ?? '',
    protocol: j['protocol'] ?? '',
    transport: j['transport'] ?? '',
    security: j['security'] ?? '',
    server: j['server'] ?? '',
    port: (j['port'] as num?)?.toInt() ?? 0,
    cores: _strings(j['cores']),
    latency: j['latency_ms'] != null || j['latency_error'] != null
        ? Latency(
            ms: (j['latency_ms'] as num?)?.toInt() ?? 0,
            error: j['latency_error'] ?? '',
            core: j['latency_core'] ?? '',
            method: j['latency_method'] ?? '',
          )
        : null,
    hidden: j['hidden'] == true,
  );
}

class SubInfo {
  final String title;
  final int upload;
  final int download;
  final int total;
  final DateTime? expire;
  final int updateIntervalHours;
  final String supportUrl;
  final String webPageUrl;

  const SubInfo({
    this.title = '',
    this.upload = 0,
    this.download = 0,
    this.total = 0,
    this.expire,
    this.updateIntervalHours = 0,
    this.supportUrl = '',
    this.webPageUrl = '',
  });

  factory SubInfo.fromJson(Json j) => SubInfo(
    title: j['title'] ?? '',
    upload: (j['upload'] as num?)?.toInt() ?? 0,
    download: (j['download'] as num?)?.toInt() ?? 0,
    total: (j['total'] as num?)?.toInt() ?? 0,
    expire: _time(j['expire']),
    updateIntervalHours: (j['update_interval_hours'] as num?)?.toInt() ?? 0,
    supportUrl: j['support_url'] ?? '',
    webPageUrl: j['web_page_url'] ?? '',
  );

  int get used => upload + download;
}

/// Whole days a subscription has left, counted up as panels count them: a
/// 30-day plan bought a minute ago has 30 days, not 29, and one ending in
/// 47 hours has 2. 0 within the last day (and after it).
int subscriptionDaysLeft(DateTime expire, [DateTime? now]) {
  final left = expire.difference(now ?? DateTime.now());
  if (left.inHours < 24) return 0;
  return (left.inMinutes / (24 * 60)).ceil();
}

/// A panel's traffic figure (limit, used, left). Panels (Remnawave, Marzban,
/// 3x-ui) count a gigabyte as 1024³ bytes: a "100 GB" plan has a limit of
/// 107 374 182 400 bytes, which must read «100 ГБ», not «107 ГБ».
String formatQuota(num b) {
  const k = 1024.0;
  String n(double v) {
    final s = v.toStringAsFixed(v >= 100 ? 0 : 1);
    return s.endsWith('.0') ? s.substring(0, s.length - 2) : s;
  }

  if (b >= k * k * k * k) return '${n(b / (k * k * k * k))} ТБ';
  if (b >= k * k * k) return '${n(b / (k * k * k))} ГБ';
  if (b >= k * k) return '${(b / (k * k)).toStringAsFixed(0)} МБ';
  return '${(b / k).toStringAsFixed(0)} КБ';
}

/// A subscription link as the daemon shows it (maskURL in
/// engine/internal/service/api_store.go): scheme and host, then "/…" and the
/// last four characters, without the access token. Links are compared in
/// this form.
String maskedUrl(String url) {
  if (url.isEmpty || url.contains('…')) return url;
  final i = url.indexOf('://');
  if (i < 0) return '…';
  var authority = url.substring(i + 3);
  var rest = '';
  final j = authority.indexOf(RegExp(r'[/?#]'));
  if (j >= 0) {
    rest = authority.substring(j);
    authority = authority.substring(0, j);
  }
  final at = authority.lastIndexOf('@');
  if (at >= 0) authority = authority.substring(at + 1);
  final head = '${url.substring(0, i).toLowerCase()}://${authority.toLowerCase()}';
  final r = rest.runes.toList();
  if (r.isEmpty || rest == '/') return head;
  if (r.length < 12) return '$head/…';
  return '$head/…${String.fromCharCodes(r.sublist(r.length - 4))}';
}

class Subscription {
  final String id;
  final String name;
  final String displayName;

  /// The link without its access token ([maskedUrl]); the whole link is
  /// asked for where it is needed, for the QR code.
  final String url;

  /// The link is plain http://: its token crosses the network as it is.
  final bool insecure;
  final String userAgent;
  final SubInfo info;
  final String format;

  /// The servers of the list; those the user removed are in [hiddenNodes].
  final List<NodeView> nodes;

  /// The servers the user removed from the list ("Удалить из подписки"):
  /// the panel still sends them, the daemon keeps them out of the latency
  /// test and of the switch to another server, until they are brought back.
  final List<NodeView> hiddenNodes;
  final List<String> skipped;

  /// The servers (fingerprints) the panel set up for automatic selection:
  /// when one stops answering, the connection moves to the next of them.
  final List<String> auto;
  final DateTime? updatedAt;
  final DateTime? checkedAt;
  final DateTime? nextUpdate;
  final String lastError;

  const Subscription({
    required this.id,
    required this.name,
    required this.displayName,
    required this.url,
    this.insecure = false,
    required this.userAgent,
    required this.info,
    required this.format,
    required this.nodes,
    this.hiddenNodes = const [],
    required this.skipped,
    this.auto = const [],
    this.updatedAt,
    this.checkedAt,
    this.nextUpdate,
    this.lastError = '',
  });

  factory Subscription.fromJson(Json j) {
    final all = ((j['nodes'] as List?) ?? []).map((n) => NodeView.fromJson((n as Map).cast())).toList();
    return Subscription._fromJson(
      j,
      [
        for (final n in all)
          if (!n.hidden) n,
      ],
      [
        for (final n in all)
          if (n.hidden) n,
      ],
    );
  }

  factory Subscription._fromJson(Json j, List<NodeView> nodes, List<NodeView> hidden) => Subscription(
    id: j['id'] ?? '',
    name: j['name'] ?? '',
    displayName: _placeholderNames[j['display_name']] ?? j['display_name'] ?? '',
    url: j['url'] ?? '',
    insecure: j['insecure'] == true,
    userAgent: j['user_agent'] ?? '',
    info: SubInfo.fromJson((j['info'] as Map?)?.cast<String, dynamic>() ?? {}),
    format: j['format'] ?? '',
    nodes: nodes,
    hiddenNodes: hidden,
    skipped: _strings(j['skipped']),
    auto: _strings(j['auto']),
    updatedAt: _time(j['updated_at']),
    checkedAt: _time(j['checked_at']),
    nextUpdate: _time(j['next_update']),
    lastError: j['last_error'] ?? '',
  );

  bool get isLocal => url.isEmpty;

  /// The panel's host, without the path that carries the access token.
  String get host => Uri.tryParse(url)?.host ?? '';
}

/// The address sites see (GET /v1/ip): the VPN server's while connected.
class IpInfo {
  final String ip;

  /// ISO code, "DE"; empty when unknown.
  final String country;
  final bool vpn;

  const IpInfo({required this.ip, this.country = '', this.vpn = false});

  factory IpInfo.fromJson(Json j) => IpInfo(ip: j['ip'] ?? '', country: j['country'] ?? '', vpn: j['vpn'] == true);
}

class Selection {
  final String subscription;
  final String fingerprint;
  final String name;
  final bool available;
  final NodeView? node;

  const Selection({this.subscription = '', this.fingerprint = '', this.name = '', this.available = false, this.node});

  factory Selection.fromJson(Json j) => Selection(
    subscription: j['subscription'] ?? '',
    fingerprint: j['fingerprint'] ?? '',
    name: j['name'] ?? '',
    available: j['available'] == true,
    node: j['node'] is Map ? NodeView.fromJson((j['node'] as Map).cast()) : null,
  );

  bool get isEmpty => subscription.isEmpty;
}

class CoreInfo {
  final String kind;
  final bool installed;
  final String version;
  final List<String> features;

  const CoreInfo({required this.kind, required this.installed, this.version = '', required this.features});

  factory CoreInfo.fromJson(Json j) =>
      CoreInfo(kind: j['kind'] ?? '', installed: j['installed'] == true, version: j['version'] ?? '', features: _strings(j['features']));
}

/// Whether a newer release of a core exists (GET /v1/cores/updates).
class CoreUpdate {
  final String kind;
  final String current;
  final String latest;
  final bool available;
  final int size;
  final String error;

  const CoreUpdate({required this.kind, this.current = '', this.latest = '', this.available = false, this.size = 0, this.error = ''});

  factory CoreUpdate.fromJson(Json j) => CoreUpdate(
    kind: j['kind'] ?? '',
    current: j['current'] ?? '',
    latest: j['latest'] ?? '',
    available: j['available'] == true,
    size: (j['size'] as num?)?.toInt() ?? 0,
    error: j['error'] ?? '',
  );
}

/// Updates of CoreShift itself (GET /v1/app-update).
class AppUpdateInfo {
  /// off, idle, checking, downloading, ready, installing, error, or
  /// available (Linux: a newer version to install from its page).
  final String state;
  final String reason;
  final String version;
  final int build;
  final String notes;
  final String error;
  final DateTime? checkedAt;

  /// Downloaded; installs by itself once the VPN is off.
  final bool waiting;

  /// New versions are installed by the user with the system's packages
  /// (Linux); [url] is the release's page to download it from.
  final bool manual;
  final String url;

  const AppUpdateInfo({
    this.state = 'off',
    this.reason = '',
    this.version = '',
    this.build = 0,
    this.notes = '',
    this.error = '',
    this.checkedAt,
    this.waiting = false,
    this.manual = false,
    this.url = '',
  });

  factory AppUpdateInfo.fromJson(Json j) => AppUpdateInfo(
    state: j['state'] ?? 'off',
    reason: j['reason'] ?? '',
    version: j['version'] ?? '',
    build: (j['build'] as num?)?.toInt() ?? 0,
    notes: j['notes'] ?? '',
    error: j['error'] ?? '',
    checkedAt: DateTime.tryParse(j['checked_at'] ?? '')?.toLocal(),
    waiting: j['waiting'] == true,
    manual: j['manual'] == true,
    url: j['url'] ?? '',
  );

  bool get off => state == 'off';
  bool get busy => state == 'checking' || state == 'downloading' || state == 'installing';
  String get label => BuildVersion(version, build).label;
}

/// The traffic of one day through the VPN (GET /v1/stats), in bytes.
class TrafficDay {
  final DateTime date;
  final int up;
  final int down;
  const TrafficDay(this.date, this.up, this.down);

  factory TrafficDay.fromJson(Json j) {
    // "2026-10-01", a local calendar day.
    final p = '${j['date']}'.split('-').map(int.tryParse).toList();
    final ok = p.length == 3 && !p.contains(null);
    return TrafficDay(ok ? DateTime(p[0]!, p[1]!, p[2]!) : DateTime.now(), (j['up'] as num?)?.toInt() ?? 0, (j['down'] as num?)?.toInt() ?? 0);
  }

  int get total => up + down;
}

/// A speed test of the connection (POST /v1/speedtest): through the VPN
/// server while connected, else of the device's own. Rates are bytes per
/// second.
class SpeedResult {
  final int downloadBps;
  final int uploadBps;
  final int latencyMs;
  final bool vpn;
  final String server;

  /// Who measured: a speedtest.net server's sponsor, or "Cloudflare"; and
  /// where the speedtest.net server is. Empty from an older service.
  final String testServer;
  final String testCity;
  final String testCountry;

  const SpeedResult({
    this.downloadBps = 0,
    this.uploadBps = 0,
    this.latencyMs = 0,
    this.vpn = false,
    this.server = '',
    this.testServer = '',
    this.testCity = '',
    this.testCountry = '',
  });

  factory SpeedResult.fromJson(Json j) => SpeedResult(
    downloadBps: (j['download_bps'] as num?)?.toInt() ?? 0,
    uploadBps: (j['upload_bps'] as num?)?.toInt() ?? 0,
    latencyMs: (j['latency_ms'] as num?)?.toInt() ?? 0,
    vpn: j['vpn'] == true,
    server: j['server'] ?? '',
    testServer: j['test_server'] ?? '',
    testCity: j['test_city'] ?? '',
    testCountry: j['test_country'] ?? '',
  );
}

/// A program running on the computer (GET /v1/apps).
class RunningApp {
  final String name;
  final String path;
  const RunningApp(this.name, this.path);

  factory RunningApp.fromJson(Json j) => RunningApp(j['name'] ?? '', j['path'] ?? '');
}

class DaemonInfo {
  final String version;
  final int build;
  final String commit;
  final List<CoreInfo> cores;
  final bool tunAvailable;
  final String tunUnavailable;

  const DaemonInfo({this.version = '', this.build = 0, this.commit = '', this.cores = const [], this.tunAvailable = false, this.tunUnavailable = ''});

  factory DaemonInfo.fromJson(Json j) => DaemonInfo(
    version: j['version'] ?? '',
    build: (j['build'] as num?)?.toInt() ?? 0,
    commit: j['commit'] ?? '',
    cores: ((j['cores'] as List?) ?? []).map((c) => CoreInfo.fromJson((c as Map).cast())).toList(),
    tunAvailable: j['tun_available'] == true,
    tunUnavailable: j['tun_unavailable'] ?? '',
  );

  bool installed(String kind) => cores.any((c) => c.kind == kind && c.installed);

  BuildVersion get buildVersion => BuildVersion(version, build, commit);

  String versionOf(String kind) => cores.where((c) => c.kind == kind).firstOrNull?.version ?? '';
}

/// The daemon's English names for an unnamed subscription.
const _placeholderNames = {'Local nodes': 'Мои серверы', 'Subscription': 'Подписка'};

/// Whether [url] is a release page on GitHub, or on its mirror on GitLab,
/// the only addresses an announced update (Linux) may send the user to.
bool isReleasePage(String url) {
  final u = Uri.tryParse(url);
  if (u == null || u.scheme != 'https' || u.hasPort || u.userInfo.isNotEmpty || u.hasQuery || u.hasFragment || u.path.contains('..')) {
    return false;
  }
  return switch (u.host) {
    'github.com' => RegExp(r'^/[A-Za-z0-9-]+/[A-Za-z0-9._-]+/releases(/[A-Za-z0-9._/-]+)?$').hasMatch(u.path),
    'gitlab.com' => RegExp(r'^(/[A-Za-z0-9][A-Za-z0-9._-]*){2,5}/-/releases(/[A-Za-z0-9._/-]+)?$').hasMatch(u.path),
    _ => false,
  };
}
