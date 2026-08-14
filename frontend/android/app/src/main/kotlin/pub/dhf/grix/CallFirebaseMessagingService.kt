package pub.dhf.grix

import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.os.Build
import androidx.core.app.NotificationCompat
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage

/**
 * 处理 FCM 高优先级来电通知（data-only message）。
 * 后端发送 type=call_invite 的 data 消息时触发此 Service。
 */
class CallFirebaseMessagingService : FirebaseMessagingService() {

    companion object {
        private const val CALL_CHANNEL_ID = "grix_incoming_call"
        private const val CALL_NOTIFICATION_ID = 9001
    }

    override fun onMessageReceived(message: RemoteMessage) {
        val data = message.data
        if (data["type"] != "call_invite") {
            // 非来电消息，不处理（由其他 FCM 处理器负责）
            return
        }

        val callId = data["call_id"] ?: return
        val callerName = data["caller_name"] ?: "Unknown"

        showIncomingCallNotification(callId, callerName)
        notifyFlutterIncomingCall(callId, callerName)
    }

    private fun showIncomingCallNotification(callId: String, callerName: String) {
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CALL_CHANNEL_ID,
                "来电通知",
                NotificationManager.IMPORTANCE_HIGH
            ).apply {
                description = "语音通话来电提醒"
                enableVibration(true)
            }
            nm.createNotificationChannel(channel)
        }

        // 点击通知打开 App
        val tapIntent = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            putExtra("call_id", callId)
            putExtra("caller_name", callerName)
            putExtra("type", "call_invite")
        }
        val pendingIntent = PendingIntent.getActivity(
            this, 0, tapIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        val notification = NotificationCompat.Builder(this, CALL_CHANNEL_ID)
            .setSmallIcon(android.R.drawable.ic_menu_call)
            .setContentTitle("来电：$callerName")
            .setContentText("语音通话")
            .setPriority(NotificationCompat.PRIORITY_HIGH)
            .setCategory(NotificationCompat.CATEGORY_CALL)
            .setFullScreenIntent(pendingIntent, true)
            .setAutoCancel(true)
            .setContentIntent(pendingIntent)
            .build()

        nm.notify(CALL_NOTIFICATION_ID, notification)
    }

    private fun notifyFlutterIncomingCall(callId: String, callerName: String) {
        // 通过广播通知 MainActivity（如果 App 在前台）
        val intent = Intent("pub.dhf.grix.CALL_INVITE").apply {
            putExtra("call_id", callId)
            putExtra("caller_name", callerName)
        }
        sendBroadcast(intent)
    }
}
