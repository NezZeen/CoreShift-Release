part of '../app_state.dart';

/// Базы категорий geosite и geoip для своих правил: в каком они состоянии
/// (загружаются, загружены, не загрузились) и что значит название,
/// набранное в редакторе правила. Состояние спрашивается, пока на экране есть
/// кто-то, кому оно нужно ([watchGeo]): раз в секунду, пока что-то грузится
/// или ждёт загрузки, иначе редко.
extension AppStateGeo on AppState {
  /// Служба умеет говорить о базах (старая — нет).
  bool get geoKnown => _geoKnown;

  /// Начинает следить за базами, пока не вызовут возвращённую функцию.
  VoidCallback watchGeo() {
    _geoWatchers++;
    _geoPoll();
    var stopped = false;
    return () {
      if (stopped) return;
      stopped = true;
      if (--_geoWatchers == 0) {
        _geoTimer?.cancel();
        _geoTimer = null;
      }
    };
  }

  Future<void> _geoPoll() async {
    if (_disposed) return;
    if (_geoBusy) {
      // Asked about while answering: what changed may not be in the answer.
      _geoAgain = true;
      return;
    }
    _geoBusy = true;
    try {
      geo = GeoStatus.fromJson(await backend.call('GET', '/v1/geo/status') as Json);
      _geoKnown = true;
      _notify();
    } on ApiError catch (e) {
      // Служба до этих запросов: баз для показа нет.
      if (e.status == 404) _geoKnown = false;
    } catch (_) {
      // Службы нет на связи: спросим снова.
    } finally {
      _geoBusy = false;
    }
    _geoTimer?.cancel();
    _geoTimer = null;
    if (_geoAgain && _geoKnown && !_disposed) {
      _geoAgain = false;
      return _geoPoll();
    }
    if (_geoWatchers > 0 && _geoKnown && !_disposed) {
      _geoTimer = Timer(Duration(seconds: geo.busy ? 1 : 15), _geoPoll);
    }
  }

  /// Что значит [name] в базе [kind] ('geosite' или 'geoip'); null, если
  /// служба не ответила.
  Future<GeoCategories?> geoCategories(String kind, String name) async {
    try {
      final q = Uri.encodeQueryComponent(name);
      final r = GeoCategories.fromJson(await backend.call('GET', '/v1/geo/categories?kind=$kind&q=$q') as Json);
      // Её только что запустили: состояние баз обновится сразу.
      if (!r.loaded) _geoPoll();
      return r;
    } catch (_) {
      return null;
    }
  }

  /// Загрузить базу заново, сейчас: «Повторить» и «Загрузить».
  Future<void> reloadGeo(String kind) async {
    try {
      geo = GeoStatus.fromJson(await backend.call('POST', '/v1/geo/reload', {'kind': kind}) as Json);
      _notify();
    } catch (e) {
      toast(humanError('$e'), ToastKind.err);
    }
    _geoPoll();
  }
}
