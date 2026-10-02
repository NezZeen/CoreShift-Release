#ifndef RUNNER_FLUTTER_WINDOW_H_
#define RUNNER_FLUTTER_WINDOW_H_

#include <flutter/dart_project.h>
#include <flutter/encodable_value.h>
#include <flutter/flutter_view_controller.h>
#include <flutter/method_channel.h>

#include <memory>

#include "win32_window.h"

// WM_COPYDATA's dwData for a link that a second start of CoreShift hands to
// the running window (main.cpp), e.g. from a panel's "add to app" button.
constexpr ULONG_PTR kOpenLinkMessage = 0x43534C4B;  // "CSLK"

// A window that does nothing but host a Flutter view.
class FlutterWindow : public Win32Window {
 public:
  // Creates a new FlutterWindow hosting a Flutter view running |project|.
  explicit FlutterWindow(const flutter::DartProject& project);
  virtual ~FlutterWindow();

 protected:
  // Win32Window:
  bool OnCreate() override;
  void OnDestroy() override;
  LRESULT MessageHandler(HWND window, UINT const message, WPARAM const wparam,
                         LPARAM const lparam) noexcept override;

 private:
  // The project to run.
  flutter::DartProject project_;

  // The Flutter instance hosted by this window.
  std::unique_ptr<flutter::FlutterViewController> flutter_controller_;

  // Tells the app of links handed over by later starts ("coreshift/desktop").
  std::unique_ptr<flutter::MethodChannel<flutter::EncodableValue>> link_channel_;
};

#endif  // RUNNER_FLUTTER_WINDOW_H_
