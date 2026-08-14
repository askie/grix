# 塘主

Flutter 跨平台管理后台，用于运营、内容审查、管理员/角色、Feature Gates、App 版本发布、Connector 升级和彩蛋管理。

## 常用命令

```bash
make web-local
make web-online
make macos-local
make macos-online
make ios-local
make ios-online
make ios-ipa-online
```

生产环境 API 默认通过 `--dart-define=ADMIN_API_BASE_URL=https://grix.dhf.pub` 注入；本地默认后端为 `http://127.0.0.1:27180`。

## Android 发布签名

本地 release 签名文件位于：

- `android/upload-keystore.jks`
- `android/key.properties`

这两个文件已被 `.gitignore` 忽略，不应提交到仓库。构建 Android 发布包：

```bash
flutter build appbundle --release --dart-define=ADMIN_API_BASE_URL=https://grix.dhf.pub
flutter build apk --release --dart-define=ADMIN_API_BASE_URL=https://grix.dhf.pub
```

## iOS TestFlight

当前 iOS 发布标识：

- Bundle ID: `pub.dhf.grixadmin`
- Team ID: `RB6MGXAF36`
- Display Name: `塘主`
- Version/Build: 来自 `pubspec.yaml` 的 `version`

构建 TestFlight 用 IPA：

```bash
make ios-ipa-online
```

上传到 App Store Connect/TestFlight 使用 Apple ID + app-specific password：

```bash
export APPLE_ID=your-apple-id@example.com
export APPLE_APP_PASSWORD=xxxx-xxxx-xxxx-xxxx
make ios-upload-testflight
```

推荐统一发布入口：

```bash
cd ..
flutter build ipa --release --dart-define=ADMIN_API_BASE_URL=https://your-grix.example
```

该入口会自动 bump `pubspec.yaml` build number、构建 IPA，并在 `APPLE_ID`/`APPLE_APP_PASSWORD` 存在时上传到 App Store Connect。`AUTO_UPLOAD_IOS_APPSTORE=0` 可只构建不上传。
