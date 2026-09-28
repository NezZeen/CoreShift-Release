package dev.coreshift.coreshift

import java.util.Locale

/**
 * The connection as the engine last reported it (Engine.VpnPlatform), for
 * the notification and the quick settings tile.
 */
object VpnStatus {
    @Volatile
    var state = "idle"
        private set

    @Volatile
    var node = ""
        private set

    /** When it connected, Unix milliseconds; 0 when not connected. */
    @Volatile
    var since = 0L
        private set

    /** Bytes per second, down and up. */
    @Volatile
    var down = 0L
        private set

    @Volatile
    var up = 0L
        private set

    val active get() = state == "connecting" || state == "connected"

    fun set(state: String, node: String, since: Long) {
        this.state = state
        this.node = node
        this.since = since
        if (state != "connected") {
            down = 0
            up = 0
        }
    }

    fun setTraffic(down: Long, up: Long) {
        this.down = down
        this.up = up
    }

    /** "2.3 Мбит/с", as the app shows speeds. */
    fun formatRate(bytesPerSecond: Long): String {
        val bits = bytesPerSecond * 8.0
        return when {
            bits >= 1e9 -> String.format(Locale.ROOT, "%.1f Гбит/с", bits / 1e9)
            bits >= 1e8 -> String.format(Locale.ROOT, "%.0f Мбит/с", bits / 1e6)
            bits >= 1e6 -> String.format(Locale.ROOT, "%.1f Мбит/с", bits / 1e6)
            bits >= 1e3 -> "${Math.round(bits / 1e3)} Кбит/с"
            bits == 0.0 -> "0"
            else -> "${Math.round(bits)} бит/с"
        }
    }
}
