/// The app's own version, stamped by the build (packaging/windows/build.ps1)
/// with --dart-define; a plain `flutter run` shows "dev".
library;

const appVersion = String.fromEnvironment('CORESHIFT_VERSION', defaultValue: 'dev');
const appBuild = int.fromEnvironment('CORESHIFT_BUILD');
const appCommit = String.fromEnvironment('CORESHIFT_COMMIT');

/// A version as the build stamps it: the number from the VERSION file, the
/// build (the commit count) and the commit, "-dirty" when built from
/// uncommitted changes.
class BuildVersion implements Comparable<BuildVersion> {
  final String version;
  final int build;
  final String commit;

  const BuildVersion(this.version, [this.build = 0, this.commit = '']);

  static const app = BuildVersion(appVersion, appBuild, appCommit);

  bool get known => version.isNotEmpty && version != 'dev';

  /// "0.2.0 (сборка 14)"; the commit is for tooltips.
  String get label => build > 0 ? '$version (сборка $build)' : version;

  /// Saved to compare with after an update.
  String get key => '$version+$build+$commit';

  static BuildVersion parseKey(String s) {
    final p = s.split('+');
    return BuildVersion(p[0], p.length > 1 ? int.tryParse(p[1]) ?? 0 : 0, p.length > 2 ? p[2] : '');
  }

  List<int> get _numbers => version.split(RegExp(r'[.\-]')).map((x) => int.tryParse(x) ?? 0).take(3).toList();

  /// Orders by the version number, then by build.
  @override
  int compareTo(BuildVersion other) {
    final a = _numbers, b = other._numbers;
    for (var i = 0; i < 3; i++) {
      final x = i < a.length ? a[i] : 0, y = i < b.length ? b[i] : 0;
      if (x != y) return x.compareTo(y);
    }
    return build.compareTo(other.build);
  }

  /// The same code: same version, build and commit.
  bool same(BuildVersion o) => version == o.version && build == o.build && commit == o.commit;

  @override
  String toString() => label;
}
