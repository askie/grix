## 1.0.0+grix.1

Vendored fork（源自 pub.dev auto_updater_windows 1.0.0，经 dependency_overrides 引入）：

* WinSparkle 0.8.1 → 0.9.3（官方发行二进制），获得 EdDSA (Ed25519) 更新签名校验能力。
* 新增 method channel 方法 `setEddsaPublicKey`（参数 `publicKey`：base64 编码的 Ed25519 公钥，须在 `setFeedURL` 之前调用），返回公钥是否有效。

## 1.0.0

* First major release.

## 0.2.1

* chore(windows): Support before-quit-for-update event

## 0.2.0

* First release.
