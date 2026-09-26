// A notification from the tray icon on Windows (Shell_NotifyIcon with
// NIF_INFO), which Windows 10 and 11 show as an ordinary toast. The tray
// icon belongs to the nativeapi package; this only adds a balloon to it.
//
// nativeapi's own notifications need the Windows App SDK, which this build
// does not ship.

import 'dart:ffi';
import 'dart:io';

import 'package:ffi/ffi.dart';

typedef _FindWindowExNative = IntPtr Function(IntPtr parent, IntPtr after, Pointer<Utf16> cls, Pointer<Utf16> title);
typedef _FindWindowEx = int Function(int parent, int after, Pointer<Utf16> cls, Pointer<Utf16> title);
typedef _WindowPidNative = Uint32 Function(IntPtr hwnd, Pointer<Uint32> pid);
typedef _WindowPid = int Function(int hwnd, Pointer<Uint32> pid);
typedef _NotifyIconNative = Int32 Function(Uint32 message, Pointer<Uint8> data);
typedef _NotifyIcon = int Function(int message, Pointer<Uint8> data);

const _hwndMessage = -3; // parent of message-only windows
const _nimModify = 1;
const _nifInfo = 0x10;
const _niifRespectQuietTime = 0x80;

// NOTIFYICONDATAW on 64-bit Windows.
const _size = 976;
const _offHwnd = 8;
const _offId = 16;
const _offFlags = 20;
const _offInfo = 304; // 256 UTF-16 units
const _offInfoTitle = 820; // 64 units
const _offInfoFlags = 948;

/// Shows [title] and [body] as a notification of tray icon [iconId].
/// Returns whether Windows accepted it; notification settings or Focus
/// Assist may still keep it quiet.
bool showTrayBalloon(int iconId, String title, String body) {
  if (!Platform.isWindows || sizeOf<IntPtr>() != 8) return false;
  try {
    final user32 = DynamicLibrary.open('user32.dll');
    final shell32 = DynamicLibrary.open('shell32.dll');
    final findWindowEx = user32.lookupFunction<_FindWindowExNative, _FindWindowEx>('FindWindowExW');
    final windowPid = user32.lookupFunction<_WindowPidNative, _WindowPid>('GetWindowThreadProcessId');
    final notifyIcon = shell32.lookupFunction<_NotifyIconNative, _NotifyIcon>('Shell_NotifyIconW');

    final hwnd = using((arena) {
      // nativeapi hosts its tray icons on this message-only window; other
      // programs may use the library too, so it must be ours.
      final cls = 'NativeApiHostWindow'.toNativeUtf16(allocator: arena);
      final owner = arena<Uint32>();
      var h = 0;
      while ((h = findWindowEx(_hwndMessage, h, cls, nullptr)) != 0) {
        windowPid(h, owner);
        if (owner.value == pid) return h;
      }
      return 0;
    });
    if (hwnd == 0) return false;

    return using((arena) {
      final data = arena<Uint8>(_size);
      void u32(int off, int v) => (data + off).cast<Uint32>().value = v;
      void text(int off, int max, String s) {
        final units = s.codeUnits.take(max - 1).toList();
        for (var i = 0; i < units.length; i++) {
          (data + off + 2 * i).cast<Uint16>().value = units[i];
        }
      }

      for (var i = 0; i < _size; i++) {
        data[i] = 0;
      }
      u32(0, _size);
      (data + _offHwnd).cast<IntPtr>().value = hwnd;
      u32(_offId, iconId);
      u32(_offFlags, _nifInfo);
      text(_offInfo, 256, body.isEmpty ? ' ' : body);
      text(_offInfoTitle, 64, title);
      u32(_offInfoFlags, _niifRespectQuietTime);
      return notifyIcon(_nimModify, data) != 0;
    });
  } catch (_) {
    return false;
  }
}
