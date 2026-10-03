#include "my_application.h"

#include <flutter_linux/flutter_linux.h>
#include <string.h>

#include "flutter/generated_plugin_registrant.h"

// CoreShift runs once per user session. GApplication makes it unique on the
// session bus: a second start (a coreshift:// link opened from a browser,
// the menu entry clicked again, autostart at sign-in) hands its command
// line to the running one and exits. The running one passes a link on to
// Dart over the "coreshift/desktop" channel, as the Windows runner does,
// and shows its window, which may be hidden in the tray.

struct _MyApplication {
  GtkApplication parent_instance;
  char** dart_entrypoint_arguments;
  // Started with --tray (autostart): the window stays hidden until the
  // user opens it from the tray icon.
  gboolean start_hidden;
  GtkWindow* window;
  FlMethodChannel* link_channel;
};

G_DEFINE_TYPE(MyApplication, my_application, GTK_TYPE_APPLICATION)

// Called when first Flutter frame received.
static void first_frame_cb(MyApplication* self, FlView* view) {
  if (!self->start_hidden) {
    gtk_widget_show(gtk_widget_get_toplevel(GTK_WIDGET(view)));
  }
}

static gboolean has_argument(gchar** argv, const gchar* arg) {
  for (gchar** a = argv; a != nullptr && *a != nullptr; a++) {
    if (g_strcmp0(*a, arg) == 0) {
      return TRUE;
    }
  }
  return FALSE;
}

// Gives the window CoreShift's icon (app/tool/make_icons.py): from the icon
// theme where the package installed it, else data/coreshift.png beside the
// executable. On Wayland the dock takes it from the desktop entry instead.
static void set_window_icon(GtkWindow* window) {
  GtkIconTheme* theme = gtk_icon_theme_get_default();
  if (theme != nullptr && gtk_icon_theme_has_icon(theme, "coreshift")) {
    gtk_window_set_icon_name(window, "coreshift");
    return;
  }
  g_autofree gchar* exe = g_file_read_link("/proc/self/exe", nullptr);
  if (exe == nullptr) {
    return;
  }
  g_autofree gchar* dir = g_path_get_dirname(exe);
  g_autofree gchar* path =
      g_build_filename(dir, "data", "coreshift.png", nullptr);
  if (!gtk_window_set_icon_from_file(window, path, nullptr)) {
    g_debug("no window icon at %s", path);
  }
}

// Creates the window and the Flutter view: once, for the first start.
static void create_window(MyApplication* self) {
  GtkWindow* window =
      GTK_WINDOW(gtk_application_window_new(GTK_APPLICATION(self)));
  self->window = window;
  // CoreShift draws its own title bar, so there is no GTK header bar here
  // and no system one either. window_manager hides the latter only once the
  // window exists, which is enough on X11 but not on Wayland: GTK tells the
  // compositor whether to decorate when the window is realized, and not
  // again, so KWin kept drawing its title bar above CoreShift's.
  gtk_window_set_decorated(window, FALSE);
  gtk_window_set_title(window, "CoreShift");
  gtk_window_set_default_size(window, 1320, 860);
  set_window_icon(window);

  g_autoptr(FlDartProject) project = fl_dart_project_new();
  fl_dart_project_set_dart_entrypoint_arguments(
      project, self->dart_entrypoint_arguments);

  FlView* view = fl_view_new(project);
  GdkRGBA background_color;
  gdk_rgba_parse(&background_color, "#000000");
  fl_view_set_background_color(view, &background_color);
  gtk_widget_show(GTK_WIDGET(view));
  gtk_container_add(GTK_CONTAINER(window), GTK_WIDGET(view));

  // Show the window when Flutter renders, unless started for the tray.
  g_signal_connect_swapped(view, "first-frame", G_CALLBACK(first_frame_cb),
                           self);
  gtk_widget_realize(GTK_WIDGET(view));

  fl_register_plugins(FL_PLUGIN_REGISTRY(view));

  FlPluginRegistrar* registrar = fl_plugin_registry_get_registrar_for_plugin(
      FL_PLUGIN_REGISTRY(view), "CoreShiftLinks");
  g_autoptr(FlStandardMethodCodec) codec = fl_standard_method_codec_new();
  self->link_channel = fl_method_channel_new(
      fl_plugin_registrar_get_messenger(registrar), "coreshift/desktop",
      FL_METHOD_CODEC(codec));
  g_object_unref(registrar);

  gtk_widget_grab_focus(GTK_WIDGET(view));
}

// Hands a link from a second start to Dart (platform_io.dart, onLink).
static void send_link(MyApplication* self, const gchar* link) {
  if (self->link_channel == nullptr || strlen(link) > 64 * 1024) {
    return;
  }
  g_autoptr(FlValue) value = fl_value_new_string(link);
  fl_method_channel_invoke_method(self->link_channel, "openLink", value,
                                  nullptr, nullptr, nullptr);
}

// Implements GApplication::activate: a start without arguments, e.g. from
// D-Bus activation.
static void my_application_activate(GApplication* application) {
  MyApplication* self = MY_APPLICATION(application);
  if (self->window == nullptr) {
    create_window(self);
    return;
  }
  gtk_widget_show(GTK_WIDGET(self->window));
  gtk_window_present(self->window);
}

// Implements GApplication::command_line. It runs in the first instance,
// for its own start and for every later one.
static gint my_application_command_line(GApplication* application,
                                        GApplicationCommandLine* cmdline) {
  MyApplication* self = MY_APPLICATION(application);
  gchar** argv = g_application_command_line_get_arguments(cmdline, nullptr);
  gchar** args = argv != nullptr && argv[0] != nullptr ? argv + 1 : argv;

  if (self->window == nullptr) {
    // The first start: its arguments, a link among them, go to Dart's main.
    g_clear_pointer(&self->dart_entrypoint_arguments, g_strfreev);
    self->dart_entrypoint_arguments = g_strdupv(args);
    self->start_hidden = has_argument(args, "--tray");
    create_window(self);
    g_strfreev(argv);
    return 0;
  }

  // A later start. Autostart while CoreShift already runs changes nothing.
  gboolean link = FALSE;
  for (gchar** a = args; a != nullptr && *a != nullptr; a++) {
    if (strstr(*a, "://") != nullptr) {
      send_link(self, *a);
      link = TRUE;
    }
  }
  if (link || !has_argument(args, "--tray")) {
    gtk_widget_show(GTK_WIDGET(self->window));
    gtk_window_present(self->window);
  }
  g_strfreev(argv);
  return 0;
}

// Implements GApplication::startup.
static void my_application_startup(GApplication* application) {
  G_APPLICATION_CLASS(my_application_parent_class)->startup(application);
}

// Implements GApplication::shutdown.
static void my_application_shutdown(GApplication* application) {
  G_APPLICATION_CLASS(my_application_parent_class)->shutdown(application);
}

// Implements GObject::dispose.
static void my_application_dispose(GObject* object) {
  MyApplication* self = MY_APPLICATION(object);
  g_clear_pointer(&self->dart_entrypoint_arguments, g_strfreev);
  g_clear_object(&self->link_channel);
  G_OBJECT_CLASS(my_application_parent_class)->dispose(object);
}

static void my_application_class_init(MyApplicationClass* klass) {
  G_APPLICATION_CLASS(klass)->activate = my_application_activate;
  G_APPLICATION_CLASS(klass)->command_line = my_application_command_line;
  G_APPLICATION_CLASS(klass)->startup = my_application_startup;
  G_APPLICATION_CLASS(klass)->shutdown = my_application_shutdown;
  G_OBJECT_CLASS(klass)->dispose = my_application_dispose;
}

static void my_application_init(MyApplication* self) {}

MyApplication* my_application_new() {
  // Set the program name to the application ID, which helps various systems
  // like GTK and desktop environments map this running application to its
  // corresponding .desktop file (dev.coreshift.coreshift.desktop).
  g_set_prgname(APPLICATION_ID);

  // Unique (no G_APPLICATION_NON_UNIQUE): see the top of this file.
  return MY_APPLICATION(g_object_new(my_application_get_type(),
                                     "application-id", APPLICATION_ID, "flags",
                                     G_APPLICATION_HANDLES_COMMAND_LINE,
                                     nullptr));
}
