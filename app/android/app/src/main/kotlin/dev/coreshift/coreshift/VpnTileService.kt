package dev.coreshift.coreshift

import android.app.PendingIntent
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import dev.coreshift.mobile.Mobile

/**
 * The CoreShift tile in the quick settings: turns the VPN on with the
 * selected server, or off, and shows which server is connected.
 */
class VpnTileService : TileService() {
    override fun onStartListening() {
        super.onStartListening()
        listening = this
        update()
    }

    override fun onStopListening() {
        if (listening === this) listening = null
        super.onStopListening()
    }

    override fun onClick() {
        super.onClick()
        Engine.start(this)
        if (VpnStatus.active) {
            Thread { Mobile.disconnect("tile") }.start()
            return
        }
        // The VPN was never allowed: that takes the app.
        if (VpnService.prepare(this) != null) {
            openApp()
            return
        }
        // In the foreground now, while the tap lets it; the engine then
        // builds the VPN in the running service.
        startForegroundService(
            Intent(this, CoreShiftVpnService::class.java).setAction(CoreShiftVpnService.ACTION_CONNECTING),
        )
        Engine.connect()
        qsTile?.let {
            it.state = Tile.STATE_ACTIVE
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) it.subtitle = "Подключение…"
            it.updateTile()
        }
    }

    private fun update() {
        val tile = qsTile ?: return
        tile.state = if (VpnStatus.active) Tile.STATE_ACTIVE else Tile.STATE_INACTIVE
        tile.label = "CoreShift"
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            tile.subtitle = when (VpnStatus.state) {
                "connected" -> VpnStatus.node.ifEmpty { "Подключено" }
                "connecting" -> "Подключение…"
                "no-network" -> "Нет сети"
                "disconnecting" -> "Отключение…"
                else -> "Выключен"
            }
        }
        tile.updateTile()
    }

    private fun openApp() {
        val intent = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startActivityAndCollapse(PendingIntent.getActivity(this, 0, intent, PendingIntent.FLAG_IMMUTABLE))
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(intent)
        }
    }

    companion object {
        @Volatile
        private var listening: VpnTileService? = null

        /** Shows the engine's new state on the tile. */
        fun refresh(context: Context) {
            listening?.let { tile -> Handler(Looper.getMainLooper()).post { tile.update() } }
            try {
                requestListeningState(context, ComponentName(context, VpnTileService::class.java))
            } catch (_: Exception) {
                // Not added to the quick settings: nothing to update.
            }
        }
    }
}
