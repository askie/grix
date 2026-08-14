#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)"
FRONTEND_DIR="$REPO_ROOT/frontend"
WEB_DIST_DIR="$REPO_ROOT/backend/internal/webapp/dist"
README_PATH="$WEB_DIST_DIR/README.md"
SYNC_FINGERPRINT_FILE_NAME=".sync_web_dist_fingerprint"
WEB_DIST_FINGERPRINT_PATH="$WEB_DIST_DIR/$SYNC_FINGERPRINT_FILE_NAME"

BUILD_MODE="${BUILD_MODE:-release}"
BASE_HREF="${BASE_HREF:-/}"
API_BASE_URL="${API_BASE_URL:-}"
WS_URL="${WS_URL:-}"
CN_API_URL="${CN_API_URL:-}"
CN_WS_URL="${CN_WS_URL:-}"
GLOBAL_API_URL="${GLOBAL_API_URL:-}"
GLOBAL_WS_URL="${GLOBAL_WS_URL:-}"
WEB_PUSH_VAPID_PUBLIC_KEY="${WEB_PUSH_VAPID_PUBLIC_KEY:-}"
STRIP_BUNDLED_WEB_FONT="${STRIP_BUNDLED_WEB_FONT:-1}"
NO_TREE_SHAKE_ICONS="${NO_TREE_SHAKE_ICONS:-0}"
WEB_USE_WASM="${WEB_USE_WASM:-1}"
WEB_UI_FONT_REL_PATH="assets/fonts/grix_ui_zh_subset.ttf"
# FORCE_WEB_REBUILD=1 forces a fresh `flutter build web`, bypassing fingerprint reuse.
FORCE_WEB_REBUILD="${FORCE_WEB_REBUILD:-0}"

TEMP_BUILD_DIR=""
BUILD_FRONTEND_DIR="$FRONTEND_DIR"
CURRENT_INPUT_FINGERPRINT=""
HASH_CMD=()

if ! command -v flutter >/dev/null 2>&1; then
	echo "flutter not found in PATH" >&2
	exit 127
fi

case "$BUILD_MODE" in
release)
	build_flag="--release"
	;;
profile)
	build_flag="--profile"
	;;
debug)
	build_flag="--debug"
	;;
*)
	echo "unsupported BUILD_MODE: $BUILD_MODE" >&2
	exit 1
	;;
esac

case "$NO_TREE_SHAKE_ICONS" in
0|1)
	;;
*)
	echo "unsupported NO_TREE_SHAKE_ICONS: $NO_TREE_SHAKE_ICONS" >&2
	exit 1
	;;
esac

case "$WEB_USE_WASM" in
0|1)
	;;
*)
	echo "unsupported WEB_USE_WASM: $WEB_USE_WASM" >&2
	exit 1
	;;
esac

case "$FORCE_WEB_REBUILD" in
0|1)
	;;
*)
	echo "unsupported FORCE_WEB_REBUILD: $FORCE_WEB_REBUILD" >&2
	exit 1
	;;
esac

if command -v sha256sum >/dev/null 2>&1; then
	HASH_CMD=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
	HASH_CMD=(shasum -a 256)
else
	echo "missing hash command: need sha256sum or shasum" >&2
	exit 127
fi

hash_stream() {
	"${HASH_CMD[@]}" | awk '{print $1}'
}

hash_file() {
	local file_path="$1"
	"${HASH_CMD[@]}" "$file_path" | awk '{print $1}'
}

compute_sync_input_fingerprint() {
	(
		cd "$FRONTEND_DIR"
		{
				printf 'SYNC_WEB_DIST_INPUT_V2\n'
			printf 'BUILD_MODE=%s\n' "$BUILD_MODE"
			printf 'BASE_HREF=%s\n' "$BASE_HREF"
			printf 'API_BASE_URL=%s\n' "$API_BASE_URL"
			printf 'WS_URL=%s\n' "$WS_URL"
			printf 'CN_API_URL=%s\n' "$CN_API_URL"
			printf 'CN_WS_URL=%s\n' "$CN_WS_URL"
			printf 'GLOBAL_API_URL=%s\n' "$GLOBAL_API_URL"
			printf 'GLOBAL_WS_URL=%s\n' "$GLOBAL_WS_URL"
			printf 'WEB_PUSH_VAPID_PUBLIC_KEY=%s\n' "$WEB_PUSH_VAPID_PUBLIC_KEY"
			printf 'STRIP_BUNDLED_WEB_FONT=%s\n' "$STRIP_BUNDLED_WEB_FONT"
			printf 'NO_TREE_SHAKE_ICONS=%s\n' "$NO_TREE_SHAKE_ICONS"
			printf 'WEB_USE_WASM=%s\n' "$WEB_USE_WASM"
			printf 'WEB_UI_FONT_REL_PATH=%s\n' "$WEB_UI_FONT_REL_PATH"
			while IFS= read -r rel_path; do
				rel_path="${rel_path#./}"
				printf 'FILE:%s\n' "$rel_path"
				hash_file "$rel_path"
			done < <(
				find . -type f \
					! -path './build/*' \
					! -path './.dart_tool/*' \
					! -path './.idea/*' \
					| LC_ALL=C sort
			)
		} | hash_stream
	)
}

read_saved_fingerprint() {
	local path="$1"
	[[ -f "$path" ]] || return 1
	tr -d '[:space:]' <"$path"
}

write_sync_fingerprint() {
	local target_dir="$1"
	mkdir -p "$target_dir"
	printf '%s\n' "$CURRENT_INPUT_FINGERPRINT" >"$target_dir/$SYNC_FINGERPRINT_FILE_NAME"
}

sync_if_fingerprint_matches() {
	local saved_fingerprint

	if [[ "$FORCE_WEB_REBUILD" == "1" ]]; then
		return 1
	fi
	if [[ ! -f "$WEB_DIST_DIR/index.html" ]]; then
		return 1
	fi
	if ! saved_fingerprint="$(read_saved_fingerprint "$WEB_DIST_FINGERPRINT_PATH")"; then
		return 1
	fi
	if [[ -z "$CURRENT_INPUT_FINGERPRINT" || "$saved_fingerprint" != "$CURRENT_INPUT_FINGERPRINT" ]]; then
		return 1
	fi

	echo "reuse existing Flutter Web bundle (fingerprint=$CURRENT_INPUT_FINGERPRINT)"
	sync_web_bundle "$WEB_DIST_DIR" "$FRONTEND_DIR/build/web"
	write_sync_fingerprint "$FRONTEND_DIR/build/web"
	echo "synced Flutter Web bundle into $WEB_DIST_DIR"
	return 0
}

cleanup() {
	if [[ -n "$TEMP_BUILD_DIR" && -d "$TEMP_BUILD_DIR" ]]; then
		rm -rf "$TEMP_BUILD_DIR"
	fi
}

strip_bundled_web_font_from_pubspec() {
	local pubspec_path="$1"
	local tmp_path="${pubspec_path}.tmp"

	awk '
BEGIN {
	in_flutter = 0
	skip_fonts = 0
}
/^flutter:$/ {
	in_flutter = 1
}
in_flutter && /^  fonts:$/ {
	skip_fonts = 1
	next
}
skip_fonts && /^  assets:$/ {
	skip_fonts = 0
}
skip_fonts {
	next
}
{
	print
}
' "$pubspec_path" >"$tmp_path"

	mv "$tmp_path" "$pubspec_path"
}

prepare_build_frontend_dir() {
	if [[ "$STRIP_BUNDLED_WEB_FONT" != "1" ]]; then
		return
	fi

	TEMP_BUILD_DIR="$(mktemp -d "${TMPDIR:-/tmp}/aibot-web-build.XXXXXX")"
	rsync -a \
		--exclude build \
		--exclude .dart_tool \
		--exclude .idea \
		"$FRONTEND_DIR/" "$TEMP_BUILD_DIR/"
	strip_bundled_web_font_from_pubspec "$TEMP_BUILD_DIR/pubspec.yaml"
	BUILD_FRONTEND_DIR="$TEMP_BUILD_DIR"
}

cleanup_web_bundle() {
	local web_dir="$1"

	find "$web_dir" -type f -name '*.symbols' -delete
	find "$web_dir" -type f -name '*.js' | while IFS= read -r f; do
		sed -i '' '/^\/\/.*sourceMappingURL=/d' "$f"
	done
}

sync_preloaded_web_font() {
	local web_dir="$1"
	local font_source="$BUILD_FRONTEND_DIR/$WEB_UI_FONT_REL_PATH"
	local font_target="$web_dir/$WEB_UI_FONT_REL_PATH"

	[[ -f "$font_source" ]] || {
		echo "missing web UI font: $font_source" >&2
		exit 1
	}

	mkdir -p "$(dirname "$font_target")"
	cp "$font_source" "$font_target"
}

inject_web_build_id() {
	local web_dir="$1"
	local build_id_path="$web_dir/.last_build_id"
	local version_path="$web_dir/version.json"
	local web_build_id

	[[ -f "$build_id_path" ]] || {
		echo "missing Flutter Web build id: $build_id_path" >&2
		exit 1
	}
	[[ -f "$version_path" ]] || {
		echo "missing Flutter Web version metadata: $version_path" >&2
		exit 1
	}
	command -v python3 >/dev/null 2>&1 || {
		echo "python3 not found in PATH" >&2
		exit 127
	}

	web_build_id="$(tr -d '[:space:]' <"$build_id_path")"
	[[ -n "$web_build_id" ]] || {
		echo "empty Flutter Web build id: $build_id_path" >&2
		exit 1
	}

	python3 - "$version_path" "$web_build_id" <<'PY'
import json
import sys

path, web_build_id = sys.argv[1:]
with open(path, "r", encoding="utf-8") as file:
    metadata = json.load(file)
metadata["web_build_id"] = web_build_id
with open(path, "w", encoding="utf-8") as file:
    json.dump(metadata, file, separators=(",", ":"))
PY
}

sync_web_bundle() {
	local source_dir="$1"
	local target_dir="$2"

	if [[ "$source_dir" != "$target_dir" ]]; then
		rm -rf "$target_dir"
		mkdir -p "$target_dir"
		rsync -a --delete "$source_dir/" "$target_dir/"
	fi

	cleanup_web_bundle "$target_dir"
	patch_font_fallback_url "$target_dir"
}

patch_font_fallback_url() {
	local web_dir="$1"
	local main_js

	main_js="$(find "$web_dir" -maxdepth 1 -name 'main.dart.js' -type f 2>/dev/null | head -1)"
	[[ -n "$main_js" ]] || return 0

	if grep -q 'https://fonts.gstatic.com/s/' "$main_js"; then
		sed -i.bak 's|https://fonts.gstatic.com/s/|font-fallbacks/|g' "$main_js"
		rm -f "${main_js}.bak"
		echo "patched font fallback URL in $(basename "$main_js")"
	fi
}

trap cleanup EXIT

CURRENT_INPUT_FINGERPRINT="$(compute_sync_input_fingerprint)"
if sync_if_fingerprint_matches; then
	exit 0
fi

prepare_build_frontend_dir

build_cmd=(
	flutter
	build
	web
	"$build_flag"
	"--base-href=$BASE_HREF"
)

if [[ "$WEB_USE_WASM" == "1" ]]; then
	build_cmd+=("--wasm")
fi

if [[ "$NO_TREE_SHAKE_ICONS" == "1" ]]; then
	build_cmd+=("--no-tree-shake-icons")
fi

if [[ -n "$API_BASE_URL" ]]; then
	build_cmd+=("--dart-define=API_BASE_URL=$API_BASE_URL")
fi
if [[ -n "$WS_URL" ]]; then
	build_cmd+=("--dart-define=WS_URL=$WS_URL")
fi
if [[ -n "$CN_API_URL" ]]; then
	build_cmd+=("--dart-define=CN_API_URL=$CN_API_URL")
fi
if [[ -n "$CN_WS_URL" ]]; then
	build_cmd+=("--dart-define=CN_WS_URL=$CN_WS_URL")
fi
if [[ -n "$GLOBAL_API_URL" ]]; then
	build_cmd+=("--dart-define=GLOBAL_API_URL=$GLOBAL_API_URL")
fi
if [[ -n "$GLOBAL_WS_URL" ]]; then
	build_cmd+=("--dart-define=GLOBAL_WS_URL=$GLOBAL_WS_URL")
fi
if [[ -n "$WEB_PUSH_VAPID_PUBLIC_KEY" ]]; then
	build_cmd+=("--dart-define=WEB_PUSH_VAPID_PUBLIC_KEY=$WEB_PUSH_VAPID_PUBLIC_KEY")
fi
if (($# > 0)); then
	build_cmd+=("$@")
fi

pushd "$BUILD_FRONTEND_DIR" >/dev/null
flutter pub get
"${build_cmd[@]}"
popd >/dev/null

sync_preloaded_web_font "$BUILD_FRONTEND_DIR/build/web"
inject_web_build_id "$BUILD_FRONTEND_DIR/build/web"
sync_web_bundle "$BUILD_FRONTEND_DIR/build/web" "$FRONTEND_DIR/build/web"
sync_web_bundle "$FRONTEND_DIR/build/web" "$WEB_DIST_DIR"
write_sync_fingerprint "$FRONTEND_DIR/build/web"
write_sync_fingerprint "$WEB_DIST_DIR"

cat >"$README_PATH" <<'EOF'
This directory stores the generated Flutter Web bundle that is embedded into the Go API binary.

Do not hand-edit generated files here. Use `backend/scripts/sync_web_dist.sh` to rebuild and sync `frontend/build/web` into this directory before compiling `cmd/api`.
EOF

echo "synced Flutter Web bundle into $WEB_DIST_DIR"
