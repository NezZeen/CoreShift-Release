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

enum ConnState { idle, connecting, connected, disconnecting, failed }

ConnState _connState(dynamic v) => switch (v) {
  'connecting' => ConnState.connecting,
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
  );

  bool get active => state == ConnState.connected || state == ConnState.connecting;
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

class Subscription {
  final String id;
  final String name;
  final String displayName;
  final String url;
  final String userAgent;
  final SubInfo info;
  final String format;
  final List<NodeView> nodes;
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
    required this.userAgent,
    required this.info,
    required this.format,
    required this.nodes,
    required this.skipped,
    this.auto = const [],
    this.updatedAt,
    this.checkedAt,
    this.nextUpdate,
    this.lastError = '',
  });

  factory Subscription.fromJson(Json j) => Subscription(
    id: j['id'] ?? '',
    name: j['name'] ?? '',
    displayName: _placeholderNames[j['display_name']] ?? j['display_name'] ?? '',
    url: j['url'] ?? '',
    userAgent: j['user_agent'] ?? '',
    info: SubInfo.fromJson((j['info'] as Map?)?.cast<String, dynamic>() ?? {}),
    format: j['format'] ?? '',
    nodes: ((j['nodes'] as List?) ?? []).map((n) => NodeView.fromJson((n as Map).cast())).toList(),
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
  /// off, idle, checking, downloading, ready, installing or error.
  final String state;
  final String reason;
  final String version;
  final int build;
  final String notes;
  final String error;
  final DateTime? checkedAt;

  /// Downloaded; installs by itself once the VPN is off.
  final bool waiting;

  const AppUpdateInfo({
    this.state = 'off',
    this.reason = '',
    this.version = '',
    this.build = 0,
    this.notes = '',
    this.error = '',
    this.checkedAt,
    this.waiting = false,
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

  const SpeedResult({this.downloadBps = 0, this.uploadBps = 0, this.latencyMs = 0, this.vpn = false, this.server = ''});

  factory SpeedResult.fromJson(Json j) => SpeedResult(
    downloadBps: (j['download_bps'] as num?)?.toInt() ?? 0,
    uploadBps: (j['upload_bps'] as num?)?.toInt() ?? 0,
    latencyMs: (j['latency_ms'] as num?)?.toInt() ?? 0,
    vpn: j['vpn'] == true,
    server: j['server'] ?? '',
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
