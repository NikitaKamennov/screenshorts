#!/bin/sh
set -eu
SCREENSHORTS_BUNDLE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
SCREENSHORTS_RUNTIME="$SCREENSHORTS_BUNDLE/runtime"
# Keep base glibc/loader on the host for the desktop; workers get a private /usr.
unset LD_PRELOAD LD_AUDIT PYTHONHOME PYTHONPATH
unset GTK_MODULES GTK3_MODULES GIO_EXTRA_MODULES
export SCREENSHORTS_HOST_DATA_DIRS="${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"
export LD_LIBRARY_PATH="$SCREENSHORTS_RUNTIME/lib"
export PATH="$SCREENSHORTS_BUNDLE/bin:/usr/local/bin:/usr/bin:/bin"
export XDG_DATA_DIRS="$SCREENSHORTS_RUNTIME/usr/share:${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"
export GSETTINGS_SCHEMA_DIR="$SCREENSHORTS_RUNTIME/usr/share/glib-2.0/schemas"
export GIO_MODULE_DIR="$SCREENSHORTS_RUNTIME/usr/lib/x86_64-linux-gnu/gio/modules"
export GDK_PIXBUF_MODULEDIR="$SCREENSHORTS_RUNTIME/usr/lib/x86_64-linux-gnu/gdk-pixbuf-2.0/2.10.0/loaders"
export GTK_PATH="$SCREENSHORTS_RUNTIME/usr/lib/x86_64-linux-gnu/gtk-3.0"
export WEBKIT_EXEC_PATH="$SCREENSHORTS_RUNTIME/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1"
export WEBKIT_INJECTED_BUNDLE_PATH="$WEBKIT_EXEC_PATH/injected-bundle"
export GST_PLUGIN_SYSTEM_PATH_1_0="$SCREENSHORTS_RUNTIME/usr/lib/x86_64-linux-gnu/gstreamer-1.0"
export LIBGL_DRIVERS_PATH="$SCREENSHORTS_RUNTIME/usr/lib/x86_64-linux-gnu/dri"
export FONTCONFIG_FILE="$SCREENSHORTS_RUNTIME/etc/fonts/fonts.conf"
# Changing the extraction path must also invalidate caches containing full paths.
SCREENSHORTS_CACHE_BASE=${XDG_CACHE_HOME:-"$HOME/.cache"}
SCREENSHORTS_PATH_ID=$(printf '%s' "$SCREENSHORTS_BUNDLE" | /usr/bin/sha256sum)
SCREENSHORTS_PATH_ID=${SCREENSHORTS_PATH_ID%% *}
SCREENSHORTS_CACHE="$SCREENSHORTS_CACHE_BASE/skrinpolka/gui-$SCREENSHORTS_PATH_ID"
umask 077
mkdir -p -- "$SCREENSHORTS_CACHE"
export GDK_PIXBUF_MODULE_FILE="$SCREENSHORTS_CACHE/loaders.cache"
if [ ! -s "$GDK_PIXBUF_MODULE_FILE" ]; then
  "$SCREENSHORTS_RUNTIME/usr/lib/x86_64-linux-gnu/gdk-pixbuf-2.0/gdk-pixbuf-query-loaders" > "$GDK_PIXBUF_MODULE_FILE.$$"
  mv -- "$GDK_PIXBUF_MODULE_FILE.$$" "$GDK_PIXBUF_MODULE_FILE"
fi
export GTK_IM_MODULE_FILE="$SCREENSHORTS_CACHE/immodules.cache"
if [ ! -s "$GTK_IM_MODULE_FILE" ]; then
  "$SCREENSHORTS_RUNTIME/usr/lib/x86_64-linux-gnu/libgtk-3-0t64/gtk-query-immodules-3.0" > "$GTK_IM_MODULE_FILE.$$"
  mv -- "$GTK_IM_MODULE_FILE.$$" "$GTK_IM_MODULE_FILE"
fi
if [ -f "$SCREENSHORTS_RUNTIME/usr/share/glvnd/egl_vendor.d/50_mesa.json" ]; then
  export __EGL_VENDOR_LIBRARY_FILENAMES="$SCREENSHORTS_RUNTIME/usr/share/glvnd/egl_vendor.d/50_mesa.json"
fi
# WebKit's patched helper names are resolved through our PATH, retaining its sandbox.
cd -- "$SCREENSHORTS_BUNDLE"
exec "$SCREENSHORTS_BUNDLE/libexec/ScreenShortsPro" --runtime-config "$SCREENSHORTS_BUNDLE/config/runtime-linux.json" "$@"
