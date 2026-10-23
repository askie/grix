# Google Play 商店资料（Grix / `pub.dhf.grix`）

对齐 App Store Connect 线上文案（Grix Ai 3.2.11）。

| 文件 | 说明 |
|---|---|
| `store_listing.json` | en-US / zh-CN 标题、短描述、完整描述、联系方式 |
| `graphics/` | `icon-512.png`、`feature-graphic-1024x500.png` |
| `screenshots/{en-US,zh-CN}/` | 手机截图（来自 ASC iPhone 6.7 素材） |

```bash
./scripts/play_fill_store_listing.py --dry-run
./scripts/play_fill_store_listing.py
```

密钥默认：`~/.config/grix/secrets/play-upload.json`。

仍需在 Play Console「应用内容」人工补齐：隐私政策 URL、内容分级（iOS 为 17+）、Data safety。
