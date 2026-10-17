package dev.coreshift.coreshift

import dev.coreshift.mobile.Mobile

/**
 * The words Android shows outside the app (the VPN's notification, the
 * quick settings tile, the share sheet) in the language the app shows:
 * the engine reads it from the app's settings (Mobile.appLanguage).
 */
object Tr {
    fun english(): Boolean = try {
        Mobile.appLanguage() == "en"
    } catch (_: Throwable) {
        false
    }

    /** [ru] or [en], by the app's language. */
    fun t(ru: String, en: String): String = if (english()) en else ru
}
