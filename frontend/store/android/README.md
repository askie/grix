# Google Play store assets

Listing copy under `listings/` mirrors the live iOS App Store localizations
(`zh-Hans` → `zh-CN`, `en-US` → `en-US`).

Sync text + contact details to Play:

```bash
frontend/scripts/android_sync_play_listing.py --dry-run
frontend/scripts/android_sync_play_listing.py
```

Play graphics (checked into `docs/play/graphics/`):

- `icon-512.png` — high-res icon (red Grix wordmark; do **not** use outdated `mipmap-*/ic_launcher.png` shrimp art)
- `feature-graphic-1024x500.png` — feature graphic

Still required in [Play Console](https://play.google.com/console/) (not covered by listing sync):

- App content: privacy policy URL (`https://grix.dhf.pub/legal/privacy-policy`)
- Content rating questionnaire (iOS is 17+ / chat + UGC)
- Data safety form
- Phone screenshots
- Category (iOS primary: Social Networking)
