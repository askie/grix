# Grix Widget Flutter Example

Minimal host app for the `grix_widget` package. It demonstrates:

- `GrixWidget.present()` full-screen presentation.
- `GrixWidget.makeView()` embedding with a controller.
- Floating launcher and unread badge.
- Callback logging.
- Local backend configuration.

## Run

```bash
flutter pub get
flutter run
```

The app defaults to `wk_test_sdk_site` and `http://127.0.0.1:27180`. Override these at build time:

```bash
flutter run \
  --dart-define=GRIX_SITE_KEY=wk_your_site_key \
  --dart-define=GRIX_API_BASE=https://gb.grix.im
```

For an Android emulator using a backend on the host machine, expose the port first:

```bash
adb reverse tcp:27180 tcp:27180
```

The app id sent by the SDK is the host bundle id / package name. Add it to the widget site's `allowed_apps` list. This example uses `com.grix.widget.example.flutter` on both platforms.

`--dart-define=GRIX_AUTO_PRESENT=true` opens the chat route automatically after startup (used for screenshot and local acceptance runs).
