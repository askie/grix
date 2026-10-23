# Google Play store assets

Listing copy under `listings/` mirrors the live iOS App Store localizations
(`zh-Hans` → `zh-CN`, `en-US` → `en-US`).

Sync text + contact details to Play:

```bash
frontend/scripts/android_sync_play_listing.py --dry-run
frontend/scripts/android_sync_play_listing.py
```

Still required in [Play Console](https://play.google.com/console/) (not covered by this sync):

- App content: privacy policy URL (`https://grix.dhf.pub/legal/privacy-policy`)
- Content rating questionnaire (iOS is 17+ / chat + UGC)
- Data safety form
- Phone screenshots + feature graphic (1024×500)
- Category (iOS primary: Social Networking)
