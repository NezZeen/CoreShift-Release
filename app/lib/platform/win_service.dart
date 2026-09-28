// What the app does with Windows itself: starts the CoreShift service, which
// the installer lets signed-in users start, and adds CoreShift to the
// programs Windows starts at sign-in ("Автозапуск").

import 'dart:ffi';
import 'dart:io';

import 'package:ffi/ffi.dart';

typedef _OpenSCManagerNative = IntPtr Function(Pointer<Utf16> machine, Pointer<Utf16> db, Uint32 access);
typedef _OpenSCManager = int Function(Pointer<Utf16> machine, Pointer<Utf16> db, int access);
typedef _OpenServiceNative = IntPtr Function(IntPtr scm, Pointer<Utf16> name, Uint32 access);
typedef _OpenService = int Function(int scm, Pointer<Utf16> name, int access);
typedef _StartServiceNative = Int32 Function(IntPtr service, Uint32 argc, Pointer<Pointer<Utf16>> argv);
typedef _StartService = int Function(int service, int argc, Pointer<Pointer<Utf16>> argv);
typedef _CloseHandleNative = Int32 Function(IntPtr h);
typedef _CloseHandle = int Function(int h);
typedef _GetLastErrorNative = Uint32 Function();
typedef _GetLastError = int Function();
typedef _RegSetKeyValueNative = Int32 Function(IntPtr key, Pointer<Utf16> sub, Pointer<Utf16> name, Uint32 type, Pointer<Utf16> data, Uint32 size);
typedef _RegSetKeyValue = int Function(int key, Pointer<Utf16> sub, Pointer<Utf16> name, int type, Pointer<Utf16> data, int size);
typedef _RegDeleteKeyValueNative = Int32 Function(IntPtr key, Pointer<Utf16> sub, Pointer<Utf16> name);
typedef _RegDeleteKeyValue = int Function(int key, Pointer<Utf16> sub, Pointer<Utf16> name);

const _scManagerConnect = 0x0001;
const _serviceStart = 0x0010;
const _errorAlreadyRunning = 1056;
const _errorFileNotFound = 2;
const _hkeyCurrentUser = 0x80000001;
const _regSz = 1;
const _runKey = r'Software\Microsoft\Windows\CurrentVersion\Run';
const _runValue = 'CoreShift';

/// Starts the CoreShift service without asking for administrator rights.
/// Returns true when it runs or is starting; false when Windows refused,
/// as it does for services installed by CoreShift before 0.4.
bool startServiceQuietly() {
  if (!Platform.isWindows) return false;
  try {
    final advapi = DynamicLibrary.open('advapi32.dll');
    final kernel = DynamicLibrary.open('kernel32.dll');
    final openSCManager = advapi.lookupFunction<_OpenSCManagerNative, _OpenSCManager>('OpenSCManagerW');
    final openService = advapi.lookupFunction<_OpenServiceNative, _OpenService>('OpenServiceW');
    final startService = advapi.lookupFunction<_StartServiceNative, _StartService>('StartServiceW');
    final closeService = advapi.lookupFunction<_CloseHandleNative, _CloseHandle>('CloseServiceHandle');
    final lastError = kernel.lookupFunction<_GetLastErrorNative, _GetLastError>('GetLastError');
    return using((arena) {
      final scm = openSCManager(nullptr, nullptr, _scManagerConnect);
      if (scm == 0) return false;
      try {
        final svc = openService(scm, 'CoreShift'.toNativeUtf16(allocator: arena), _serviceStart);
        if (svc == 0) return false;
        try {
          return startService(svc, 0, nullptr) != 0 || lastError() == _errorAlreadyRunning;
        } finally {
          closeService(svc);
        }
      } finally {
        closeService(scm);
      }
    });
  } catch (_) {
    return false;
  }
}

/// Makes Windows start CoreShift, in the tray, when the user signs in, or
/// stops it doing so. Returns whether that worked.
bool setAutostart(bool on) {
  if (!Platform.isWindows) return false;
  try {
    final advapi = DynamicLibrary.open('advapi32.dll');
    return using((arena) {
      final sub = _runKey.toNativeUtf16(allocator: arena);
      final name = _runValue.toNativeUtf16(allocator: arena);
      if (!on) {
        final delete = advapi.lookupFunction<_RegDeleteKeyValueNative, _RegDeleteKeyValue>('RegDeleteKeyValueW');
        final r = delete(_hkeyCurrentUser, sub, name);
        return r == 0 || r == _errorFileNotFound;
      }
      final set = advapi.lookupFunction<_RegSetKeyValueNative, _RegSetKeyValue>('RegSetKeyValueW');
      final command = '"${Platform.resolvedExecutable}" --tray';
      final data = command.toNativeUtf16(allocator: arena);
      return set(_hkeyCurrentUser, sub, name, _regSz, data, (command.length + 1) * 2) == 0;
    });
  } catch (_) {
    return false;
  }
}
