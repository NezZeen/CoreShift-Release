package dev.coreshift.coreshift

import android.app.Application

/** Starts the engine with the process, before any screen or the VPN. */
class CoreShiftApp : Application() {
    override fun onCreate() {
        super.onCreate()
        Engine.start(this)
    }
}
