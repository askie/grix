# `.well-known` 手工部署产物

该目录用于托管扫码加好友深链校验文件，目标路径为：

- `/.well-known/apple-app-site-association`
- `/.well-known/assetlinks.json`

默认模板位于当前目录下的 `.well-known/`。
模板中的签名值仅用于示例，发布前必须重新生成为线上真实签名。

如果需要按当前发布参数重新生成，请执行：

```bash
cd backend
AIBOT_SERVER_DEEP_LINK_IOS_APP_ID=TEAMID.pub.dhf.grix \
AIBOT_SERVER_DEEP_LINK_ANDROID_PACKAGE=pub.dhf.grix \
AIBOT_SERVER_DEEP_LINK_ANDROID_SHA256_CERTS=00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF \
./scripts/generate_well_known.sh
```

生成完成后，将 `backend/deploy/well-known/.well-known/` 整个目录手工上传到域名站点根目录。
