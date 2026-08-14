# Third-party notices

Grix depends on third-party packages declared in `backend/go.mod`,
`frontend/pubspec.yaml`, `admin/pubspec.yaml`, and
`voicebridge/requirements.txt`. Those packages remain subject to their own
licenses.

The repository also redistributes the following assets or source bundles:

- `backend/internal/pkg/ipgeo/data/ip2region.xdb` and the ip2region Go client:
  ip2region, Apache License 2.0.
- `backend/internal/publicsite/assets/widget/livekit-client.umd.min.js`:
  LiveKit Client SDK for JS, Apache License 2.0.
- `frontend/plugins/auto_updater_windows/`: auto_updater and WinSparkle. Their
  MIT and bundled dependency license texts are retained in that directory.
- Noto font files under `frontend/web/font-fallbacks/` and the generated
  `grix_ui_zh_subset.ttf`: Noto fonts, SIL Open Font License 1.1.
- Roboto font files under `admin/assets/fonts/`: Roboto, Apache License 2.0.

Copyright and trademark rights remain with their respective owners. This file
is informational and does not replace the license texts distributed with each
component.
