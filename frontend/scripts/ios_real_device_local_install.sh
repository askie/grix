#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
FRONTEND_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

BUILD_MODE="${BUILD_MODE:-profile}"
API_PORT="${API_PORT:-27180}"
WS_PORT="${WS_PORT:-27189}"
MAC_LAN_IP="${MAC_LAN_IP:-}"
IOS_DEVICE_ID="${IOS_DEVICE_ID:-}"
IOS_COREDEVICE_ID="${IOS_COREDEVICE_ID:-}"
BUNDLE_ID="${BUNDLE_ID:-}"

if [[ -z "$MAC_LAN_IP" ]]; then
  MAC_LAN_IP="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || true)"
fi

if [[ -z "$MAC_LAN_IP" ]]; then
  echo "Unable to detect Mac LAN IP. Set MAC_LAN_IP manually and retry." >&2
  exit 1
fi

API_ORIGIN="http://${MAC_LAN_IP}:${API_PORT}"
API_BASE_URL="${API_BASE_URL:-${API_ORIGIN}/v1}"
WS_URL="${WS_URL:-ws://${MAC_LAN_IP}:${WS_PORT}/ws}"

if [[ -z "$IOS_DEVICE_ID" ]]; then
  IOS_DEVICE_ID="$(
    flutter devices --machine | /usr/bin/python3 -c '
import json, sys
devices = json.load(sys.stdin)
physical = [
    d for d in devices
    if not d.get("emulator", False)
    and d.get("id")
    and (d.get("platformType") == "ios" or str(d.get("targetPlatform", "")).startswith("ios"))
]
if not physical:
    print("no connected physical iPhone device found", file=sys.stderr)
    sys.exit(1)
if len(physical) > 1:
    print(
        "multiple connected physical iPhone devices found: "
        + ", ".join("{}({})".format(d.get("name", "unknown"), d["id"]) for d in physical),
        file=sys.stderr,
    )
    sys.exit(2)
print(physical[0]["id"])
')"
fi

if [[ -z "$IOS_COREDEVICE_ID" ]]; then
  coredevice_ids=()
  while IFS= read -r coredevice_id; do
    if [[ -n "$coredevice_id" ]]; then
      coredevice_ids+=("$coredevice_id")
    fi
  done < <(
    xcrun devicectl list devices | awk 'NR > 2 && NF >= 4 && $4 == "connected" { print $3 }'
  )
  if [[ "${#coredevice_ids[@]}" -eq 0 ]]; then
    echo "Unable to detect connected CoreDevice id. Set IOS_COREDEVICE_ID manually and retry." >&2
    exit 1
  fi
  if [[ "${#coredevice_ids[@]}" -gt 1 ]]; then
    echo "Multiple connected CoreDevice ids found: ${coredevice_ids[*]}" >&2
    echo "Set IOS_COREDEVICE_ID manually and retry." >&2
    exit 1
  fi
  IOS_COREDEVICE_ID="${coredevice_ids[0]}"
fi

if [[ -z "$BUNDLE_ID" ]]; then
  BUNDLE_ID="$(
    sed -n 's/.*PRODUCT_BUNDLE_IDENTIFIER = \([^;]*\);/\1/p' \
      "$FRONTEND_DIR/ios/Runner.xcodeproj/project.pbxproj" | head -n 1
  )"
fi

if [[ -z "$BUNDLE_ID" ]]; then
  echo "Unable to detect iOS bundle id. Set BUNDLE_ID manually and retry." >&2
  exit 1
fi

echo "Mac LAN IP: $MAC_LAN_IP"
echo "API: $API_BASE_URL"
echo "WS: $WS_URL"
echo "Flutter device: $IOS_DEVICE_ID"
echo "CoreDevice: $IOS_COREDEVICE_ID"
echo "Bundle id: $BUNDLE_ID"

echo "Checking local backend health..."
curl -fsS "${API_ORIGIN}/health" >/dev/null
echo "Local backend is reachable."

cd "$FRONTEND_DIR"
echo "Installing $BUILD_MODE build to iPhone..."
echo "Wait for the app to finish booting, then press q in this terminal."
flutter run "--${BUILD_MODE}" -d "$IOS_DEVICE_ID" \
  --dart-define="API_BASE_URL=${API_BASE_URL}" \
  --dart-define="WS_URL=${WS_URL}"

echo "Relaunching installed app and leaving it active on the iPhone..."
xcrun devicectl device process launch \
  --device "$IOS_COREDEVICE_ID" \
  --terminate-existing \
  --activate \
  "$BUNDLE_ID" >/dev/null

echo "Done."
