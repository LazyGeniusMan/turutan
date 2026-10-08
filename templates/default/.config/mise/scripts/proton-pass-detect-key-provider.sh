#!/usr/bin/env bash
if { [ -n "${DBUS_SESSION_BUS_ADDRESS:-}" ] && command -v dbus-send >/dev/null 2>&1 && \
     dbus-send --session --dest=org.freedesktop.DBus --type=method_call --print-reply /org/freedesktop/DBus org.freedesktop.DBus.GetId >/dev/null 2>&1; } || \
   { command -v busctl >/dev/null 2>&1 && busctl --user status >/dev/null 2>&1; }; then
  printf "dbus"
elif command -v keyctl >/dev/null 2>&1 || [ -f /proc/keys ]; then
  printf "kernel"
else
  printf "fs"
fi
