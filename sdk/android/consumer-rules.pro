# Keep GrixBridge JavascriptInterface methods for WebView
-keepclassmembers class im.grix.widget.GrixJavaScriptBridge {
    @android.webkit.JavascriptInterface <methods>;
}
