#!/usr/bin/env python3
"""A stand-in for the desktop's notification service (org.freedesktop.Notifications)
for the GUI tests: it records Notify and CloseNotification calls, one per line, in the
log file given as the only argument. Run it on a private bus (dbus-run-session)."""
import sys

import dbus
import dbus.mainloop.glib
import dbus.service
from gi.repository import GLib

NAME = "org.freedesktop.Notifications"
PATH = "/org/freedesktop/Notifications"


class Notifications(dbus.service.Object):
    def __init__(self, bus, log):
        super().__init__(bus, PATH)
        self.log = log
        self.next = 0

    def write(self, line):
        with open(self.log, "a") as f:
            f.write(line + "\n")

    @dbus.service.method(NAME, in_signature="susssasa{sv}i", out_signature="u")
    def Notify(self, app, replaces, icon, summary, body, actions, hints, timeout):
        self.next += 1
        self.write(f"NOTIFY {self.next}\t{summary}\t{body}")
        return self.next

    @dbus.service.method(NAME, in_signature="u", out_signature="")
    def CloseNotification(self, nid):
        self.write(f"CLOSE {nid}")

    @dbus.service.method(NAME, in_signature="", out_signature="as")
    def GetCapabilities(self):
        return ["body", "actions"]

    @dbus.service.method(NAME, in_signature="", out_signature="ssss")
    def GetServerInformation(self):
        return ("fake-notifyd", "session-workbench tests", "1", "1.2")


dbus.mainloop.glib.DBusGMainLoop(set_as_default=True)
bus = dbus.SessionBus()
name = dbus.service.BusName(NAME, bus)
Notifications(bus, sys.argv[1])
GLib.MainLoop().run()
