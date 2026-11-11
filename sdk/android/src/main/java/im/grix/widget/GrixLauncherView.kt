package im.grix.widget

import android.content.Context
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Build
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.widget.FrameLayout
import android.widget.TextView

/**
 * Floating launcher button with dynamic `themeColor` and unread badge count.
 */
class GrixLauncherView(context: Context) : FrameLayout(context) {
    private val circleButton = TextView(context)
    private val badgeView = TextView(context)

    init {
        val sizePx = dp(56f)
        layoutParams = LayoutParams(sizePx, sizePx).apply {
            gravity = Gravity.BOTTOM or Gravity.END
            val marginPx = dp(20f)
            setMargins(marginPx, marginPx, marginPx, marginPx)
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.LOLLIPOP) {
            elevation = dp(6f).toFloat()
        }

        circleButton.apply {
            text = "💬"
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 22f)
            setTextColor(Color.WHITE)
            gravity = Gravity.CENTER
            background = createCircleDrawable(DEFAULT_THEME_COLOR)
        }
        addView(circleButton, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))

        badgeView.apply {
            setTextSize(TypedValue.COMPLEX_UNIT_SP, 11f)
            setTypeface(typeface, Typeface.BOLD)
            setTextColor(Color.WHITE)
            gravity = Gravity.CENTER
            val hPad = dp(5f)
            setPadding(hPad, 0, hPad, 0)
            background = GradientDrawable().apply {
                shape = GradientDrawable.RECTANGLE
                cornerRadius = dp(10f).toFloat()
                setColor(Color.parseColor("#EF4444"))
            }
            visibility = View.GONE
        }
        val badgeHeight = dp(20f)
        addView(
            badgeView,
            LayoutParams(LayoutParams.WRAP_CONTENT, badgeHeight).apply {
                gravity = Gravity.TOP or Gravity.END
                minimumWidth = badgeHeight
            }
        )
    }

    fun updateThemeColor(hexColor: String?) {
        val parsed = parseColorOrDefault(hexColor)
        circleButton.background = createCircleDrawable(parsed)
    }

    fun updateUnreadCount(count: Int) {
        if (count <= 0) {
            badgeView.visibility = View.GONE
            badgeView.text = ""
        } else {
            badgeView.text = if (count > 99) "99+" else count.toString()
            badgeView.visibility = View.VISIBLE
        }
    }

    private fun createCircleDrawable(color: Int): GradientDrawable {
        return GradientDrawable().apply {
            shape = GradientDrawable.OVAL
            setColor(color)
        }
    }

    private fun parseColorOrDefault(raw: String?): Int {
        val trimmed = raw?.trim().orEmpty()
        if (trimmed.isEmpty()) return DEFAULT_THEME_COLOR
        val normalized = if (trimmed.startsWith("#")) trimmed else "#$trimmed"
        return try {
            Color.parseColor(normalized)
        } catch (_: Throwable) {
            DEFAULT_THEME_COLOR
        }
    }

    private fun dp(value: Float): Int {
        return TypedValue.applyDimension(
            TypedValue.COMPLEX_UNIT_DIP,
            value,
            resources.displayMetrics
        ).toInt()
    }

    companion object {
        private val DEFAULT_THEME_COLOR = Color.parseColor("#0F766E")
    }
}
