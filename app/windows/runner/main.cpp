#include <flutter/dart_project.h>
#include <flutter/flutter_view_controller.h>
#include <windows.h>

#include <string>

#include "flutter_window.h"
#include "utils.h"

// The link CoreShift was started with, if any: a panel's "add to app"
// button opens coreshift://… (registered by the installer).
static std::string LinkArgument() {
  for (const auto &arg : GetCommandLineArguments()) {
    if (arg.find("://") != std::string::npos) {
      return arg;
    }
  }
  return "";
}

// Brings forward the window of a CoreShift that is already running, which
// may be hidden in the tray, and hands it |link| if there is one. Returns
// false if there is no such window.
static bool ActivateRunningInstance(const std::string &link) {
  HWND window = ::FindWindow(L"FLUTTER_RUNNER_WIN32_WINDOW", L"CoreShift");
  if (window == nullptr) {
    return false;
  }
  if (!link.empty()) {
    COPYDATASTRUCT data{kOpenLinkMessage, static_cast<DWORD>(link.size()),
                        const_cast<char *>(link.data())};
    ::SendMessage(window, WM_COPYDATA, 0, reinterpret_cast<LPARAM>(&data));
  }
  ::ShowWindow(window, ::IsIconic(window) ? SW_RESTORE : SW_SHOW);
  ::SetForegroundWindow(window);
  return true;
}

int APIENTRY wWinMain(_In_ HINSTANCE instance, _In_opt_ HINSTANCE prev,
                      _In_ wchar_t *command_line, _In_ int show_command) {
  // One window per user: starting CoreShift again shows the running one.
  HANDLE instance_mutex =
      ::CreateMutex(nullptr, TRUE, L"Local\\CoreShift.UI.SingleInstance");
  if (::GetLastError() == ERROR_ALREADY_EXISTS) {
    // Started for the tray, where the running one already is.
    if (wcsstr(command_line, L"--tray") != nullptr) {
      return EXIT_SUCCESS;
    }
    // The first instance may still be creating its window.
    const std::string link = LinkArgument();
    for (int i = 0; i < 20 && !ActivateRunningInstance(link); i++) {
      ::Sleep(100);
    }
    return EXIT_SUCCESS;
  }


  // Attach to console when present (e.g., 'flutter run') or create a
  // new console when running with a debugger.
  if (!::AttachConsole(ATTACH_PARENT_PROCESS) && ::IsDebuggerPresent()) {
    CreateAndAttachConsole();
  }

  // Initialize COM, so that it is available for use in the library and/or
  // plugins.
  ::CoInitializeEx(nullptr, COINIT_APARTMENTTHREADED);

  flutter::DartProject project(L"data");

  std::vector<std::string> command_line_arguments =
      GetCommandLineArguments();

  project.set_dart_entrypoint_arguments(std::move(command_line_arguments));

  FlutterWindow window(project);
  Win32Window::Point origin(10, 10);
  Win32Window::Size size(1320, 860);
  if (!window.Create(L"CoreShift", origin, size)) {
    return EXIT_FAILURE;
  }
  window.SetQuitOnClose(true);

  ::MSG msg;
  while (::GetMessage(&msg, nullptr, 0, 0)) {
    ::TranslateMessage(&msg);
    ::DispatchMessage(&msg);
  }

  ::CoUninitialize();
  if (instance_mutex != nullptr) {
    ::CloseHandle(instance_mutex);
  }
  return EXIT_SUCCESS;
}
