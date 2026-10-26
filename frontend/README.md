# frontend

## Toolchain version lock (2026-04-11)

The following versions are the current validated baseline for iOS real-device development.
Do not upgrade them casually during feature development.

- Flutter: `3.47.5` (Dart `3.13.4`)
- Xcode: `27.1` (Build `27A9275`)
- iPhoneOS SDK: `27.1`
- CocoaPods: `1.16.2`
- Ruby: `2.6.10`
- iOS deployment target (`Runner`): `15.0`
- Critical Dart deps:
  - `intl: ^0.20.2`
  - `flutter_math_fork: ^0.7.4`

Quick verification commands:

```bash
cd /path/to/grix
./scripts/check_frontend_toolchain.sh

flutter --version
xcodebuild -version
xcrun --sdk iphoneos --show-sdk-version
pod --version
ruby -v
```

Upgrade gate (mandatory):

1. Run `./scripts/check_frontend_toolchain.sh` and ensure it passes.
2. Complete both `flutter run -d <real-device-id>` and `flutter run --release -d <real-device-id>`.
3. Confirm app bootstrap logs complete and WebSocket auth succeeds.
4. Update this file and related iOS debug/deployment docs in the same commit.

Known limitation:

- Camera occlusion and physical hinge behavior still require verification on a physical iPhone Duo. Simulator resizing and posture checks do not replace real-device verification.

## iPhone Duo verification

Use Xcode 27.1 and the iOS 27.1 iPhone Duo simulator. Device Hub can open, close, partially fold, and rotate the device and exercise Split View. The app bridges iOS reserved regions through `foldable`; fully open and closed postures use the whole window, while a partially folded device places the current navigation and popup routes in an unobstructed subscreen.

From `frontend/`, build and install with the simulator's explicit device ID (required by the watch companion target):

```bash
flutter build ios --simulator --debug -d <duo-simulator-id>
xcrun simctl install <duo-simulator-id> build/ios/iphonesimulator/Grix.app
xcrun simctl launch <duo-simulator-id> pub.dhf.grix
```

If the `hinge` CLI is installed, it can drive the same posture checks:

```bash
hinge -d <duo-simulator-id> open
hinge -d <duo-simulator-id> half
hinge -d <duo-simulator-id> close
```

## Local development

Backend defaults:
- API: `http://127.0.0.1:27180/v1`
- WS: `ws://127.0.0.1:27189/ws`

Start backend first:

```bash
cd ../backend
make dev-up
```

If you debug Web with a fixed Chrome port (for example `dashmon -d chrome --web-port=34123`), start backend with the same `WEB_PORT` so WS origin checks include that local origin:

```bash
cd ../backend
WEB_PORT=34123 make dev-up
```

Run Web locally:

```bash
make web-local
```

Run Web locally with Dashmon (fixed host/port + local backend endpoints):

```bash
WEB_PORT=34123 make web-local-dashmon
```

Run macOS locally with Dashmon:

```bash
make macos-local-dashmon
```

Run Windows locally with Dashmon:

```bash
make windows-local-dashmon
```

Run Web against the online environment:

```bash
make web-test-online
```

`make web-test-online` fixes the browser origin to:

```text
http://127.0.0.1:34123
```

The online backend must include that exact origin in `AIBOT_SERVER_ALLOWED_WEB_ORIGINS`. For example:

```env
AIBOT_SERVER_ALLOWED_WEB_ORIGINS=https://grix.dhf.pub,http://127.0.0.1:34123
```

If you change `WEB_HOSTNAME` or `WEB_PORT`, update the whitelist to the exact same origin.

Run macOS against the online environment:

```bash
make macos-test-online
```

The macOS desktop app is not browser-based, so it does not require `AIBOT_SERVER_ALLOWED_WEB_ORIGINS`.

Run iPhone real-device debug against the online environment:

```bash
make ios-test-online
```

This target auto-detects the single connected physical iPhone from `flutter devices --machine`.
If multiple iPhones are connected, pass the exact id explicitly:

```bash
make ios-test-online IOS_DEVICE_ID=<device-id>
```

Do not run Chrome Web without these `--dart-define` values when using `flutter run`.
Otherwise the browser origin becomes the Flutter debug port, and `/v1/...` requests will hit the debug server itself and return `index.html`.

For iPhone real-device debugging against the online environment:

- use `https://grix.dhf.pub/v1` and `wss://grix.dhf.pub/ws` directly
- no `AIBOT_SERVER_ALLOWED_WEB_ORIGINS` adjustment is needed
- no LAN IP replacement is needed
- local-network permission is not required unless you are calling a Mac-hosted local backend

If you access the embedded Web app served by backend instead, open:

```text
http://127.0.0.1:27180/
```

In that mode, same-origin `/v1` and `/ws` resolution is correct and no extra `--dart-define` is required.

## Release builds

All commands below run from `frontend/` and build against the online environment by default.

```bash
make build-windows  # Windows release app
make build-macos    # macOS release app
make build-linux    # Linux release app
make build-ios      # iOS release app without codesign
make build-apk      # Android release APK
```

`make build-windows` only compiles the Windows app. To keep the network-share copy step explicit, use:

```bash
make deploy-windows-share
```

Platform notes:

- Windows builds must run on Windows with Flutter desktop and Visual Studio C++ desktop tooling installed.
- macOS and iOS builds must run on macOS with Xcode installed.
- Linux builds must run on Linux with Flutter desktop build dependencies installed.
- `build-ios` uses `--no-codesign`; use the release pipeline or Xcode signing flow for distributable iOS artifacts.
