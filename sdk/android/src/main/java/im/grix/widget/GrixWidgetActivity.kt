package im.grix.widget

import android.app.Activity
import android.content.pm.PackageManager
import android.os.Bundle

/**
 * Full-screen modal `Activity` launched by [GrixWidget.present].
 * Automatically forwards runtime `RECORD_AUDIO` permission results and lifecycle visibility events.
 */
class GrixWidgetActivity : Activity() {
    private var widgetView: GrixWidgetView? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val cfg = GrixWidget.config ?: GrixConfig(siteKey = "")
        val store = GrixWidget.resolveVisitorStore(this)
        val view = GrixWidgetView(
            context = this,
            config = cfg,
            visitorStore = store,
            onEvent = { event ->
                if (event is GrixBridgeEvent.Close) {
                    finish()
                }
                GrixWidget.handleBridgeEvent(this, event)
            }
        )
        widgetView = view
        setContentView(view)
    }

    override fun onResume() {
        super.onResume()
        widgetView?.notifyWebVisibility(true)
    }

    override fun onPause() {
        widgetView?.notifyWebVisibility(false)
        super.onPause()
    }

    override fun onRequestPermissionsResult(
        requestCode: Int,
        permissions: Array<out String>,
        grantResults: IntArray
    ) {
        super.onRequestPermissionsResult(requestCode, permissions, grantResults)
        if (requestCode == GrixWidgetView.REQUEST_CODE_RECORD_AUDIO) {
            val granted = grantResults.isNotEmpty() && grantResults[0] == PackageManager.PERMISSION_GRANTED
            widgetView?.onRecordAudioPermissionResult(granted)
        }
    }
}
