package dev.coreshift.coreshift

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent

/**
 * Notifications about a subscription running out: from the engine while
 * the app is closed, from the app while it is open. A tap opens CoreShift.
 */
object Alerts {
    private const val CHANNEL = "alerts"

    fun notify(context: Context, title: String, body: String) {
        val nm = context.getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, "Подписка", NotificationManager.IMPORTANCE_DEFAULT).apply {
                description = "Окончание срока и трафика подписки"
            },
        )
        val open = PendingIntent.getActivity(context, 2, Intent(context, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val n = Notification.Builder(context, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_vpn)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(Notification.BigTextStyle().bigText(body))
            .setContentIntent(open)
            .setAutoCancel(true)
            .build()
        // One notification per title: the same warning, from the engine
        // and from the app, replaces itself.
        nm.notify(title.hashCode(), n)
    }
}
