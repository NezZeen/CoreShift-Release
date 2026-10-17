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

    /**
     * The connection is the proxy without the VPN («Прокси без VPN»): no
     * VpnService is built, the service only keeps the engine alive.
     */
    @Volatile
    var proxyOnly = false
        private set

    /** "no-network": waiting for a network to connect, or held until it returns. */
    val active get() = state == "connecting" || state == "connected" || state == "no-network"

    fun set(state: String, node: String, since: Long, proxyOnly: Boolean) {
        this.state = state
        this.node = node
        this.since = since
        this.proxyOnly = proxyOnly
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
            bits >= 1e9 -> String.format(Locale.ROOT, "%.1f %s", bits / 1e9, Tr.t("Гбит/с", "Gbit/s"))
            bits >= 1e8 -> String.format(Locale.ROOT, "%.0f %s", bits / 1e6, Tr.t("Мбит/с", "Mbit/s"))
            bits >= 1e6 -> String.format(Locale.ROOT, "%.1f %s", bits / 1e6, Tr.t("Мбит/с", "Mbit/s"))
            bits >= 1e3 -> "${Math.round(bits / 1e3)} " + Tr.t("Кбит/с", "kbit/s")
            bits == 0.0 -> "0"
            else -> "${Math.round(bits)} " + Tr.t("бит/с", "bit/s")
        }
    }
}
