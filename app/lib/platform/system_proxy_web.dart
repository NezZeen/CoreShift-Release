// The web preview sets no system proxy.

/// Whether this system can have its proxy set by CoreShift.
bool get systemProxySupported => false;

/// Runs `coreshiftd sysproxy <args>` and returns its report as JSON.
Future<Map<String, dynamic>> runSystemProxy(List<String> args) async => {
  'action': args.isEmpty ? '' : args.first,
  'errors': ['недоступно в браузере'],
};

/// Whether a journal of settings to put back is there.
bool systemProxyJournalExists() => false;
