#!/usr/bin/env bash
set -euo pipefail

# Builds the admin (塘主) Flutter Web bundle and syncs it into
# backend/internal/adminweb/dist so it gets embedded into the api binary and
# served under /admin. Mirrors sync_web_dist.sh but targets the admin app.

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/../.." && pwd)"

ADMIN_DIR="$REPO_ROOT/admin"
DIST_DIR="$REPO_ROOT/backend/internal/adminweb/dist"
README_PATH="$DIST_DIR/README.md"
SYNC_FINGERPRINT_FILE_NAME=".sync_admin_dist_fingerprint"
DIST_FINGERPRINT_PATH="$DIST_DIR/$SYNC_FINGERPRINT_FILE_NAME"

BUILD_MODE="${BUILD_MODE:-release}"
# Admin SPA is mounted under /admin; base-href must carry that prefix.
BASE_HREF="${ADMIN_BASE_HREF:-/admin/}"
ADMIN_API_BASE_URL="${ADMIN_API_BASE_URL:-}"
NO_TREE_SHAKE_ICONS="${NO_TREE_SHAKE_ICONS:-0}"
# dart2js by default; dart2wasm pulls dart:ffi (win32) deps that break the web build.
WEB_USE_WASM="${ADMIN_WEB_USE_WASM:-0}"
# FORCE_ADMIN_REBUILD=1 forces a fresh build, bypassing fingerprint reuse.
FORCE_ADMIN_REBUILD="${FORCE_ADMIN_REBUILD:-0}"

if ! command -v flutter >/dev/null 2>&1; then
	echo "flutter not found in PATH" >&2
	exit 127
fi

case "$BUILD_MODE" in
release) build_flag="--release" ;;
profile) build_flag="--profile" ;;
debug) build_flag="--debug" ;;
*)
	echo "unsupported BUILD_MODE: $BUILD_MODE" >&2
	exit 1
	;;
esac

for flag_name in NO_TREE_SHAKE_ICONS WEB_USE_WASM FORCE_ADMIN_REBUILD; do
	val="${!flag_name}"
	case "$val" in
	0 | 1) ;;
	*)
		echo "unsupported $flag_name: $val" >&2
		exit 1
		;;
	esac
done

if command -v sha256sum >/dev/null 2>&1; then
	HASH_CMD=(sha256sum)
elif command -v shasum >/dev/null 2>&1; then
	HASH_CMD=(shasum -a 256)
else
	echo "missing hash command: need sha256sum or shasum" >&2
	exit 127
fi

hash_stream() { "${HASH_CMD[@]}" | awk '{print $1}'; }
hash_file() { "${HASH_CMD[@]}" "$1" | awk '{print $1}'; }

compute_input_fingerprint() {
	(
		cd "$ADMIN_DIR"
		{
			printf 'SYNC_ADMIN_DIST_INPUT_V1\n'
			printf 'BUILD_MODE=%s\n' "$BUILD_MODE"
			printf 'BASE_HREF=%s\n' "$BASE_HREF"
			printf 'ADMIN_API_BASE_URL=%s\n' "$ADMIN_API_BASE_URL"
			printf 'NO_TREE_SHAKE_ICONS=%s\n' "$NO_TREE_SHAKE_ICONS"
			printf 'WEB_USE_WASM=%s\n' "$WEB_USE_WASM"
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
	[[ -f "$1" ]] || return 1
	tr -d '[:space:]' <"$1"
}

write_fingerprint() {
	local target_dir="$1"
	mkdir -p "$target_dir"
	printf '%s\n' "$CURRENT_INPUT_FINGERPRINT" >"$target_dir/$SYNC_FINGERPRINT_FILE_NAME"
}

cleanup_web_bundle() {
	local web_dir="$1"
	find "$web_dir" -type f -name '*.symbols' -delete
	find "$web_dir" -type f -name '*.js' | while IFS= read -r f; do
		sed -i '' '/^\/\/.*sourceMappingURL=/d' "$f" 2>/dev/null || \
			sed -i '/^\/\/.*sourceMappingURL=/d' "$f"
	done
}

sync_web_bundle() {
	local source_dir="$1"
	local target_dir="$2"
	rm -rf "$target_dir"
	mkdir -p "$target_dir"
	rsync -a --delete "$source_dir/" "$target_dir/"
	cleanup_web_bundle "$target_dir"
}

write_readme() {
	cat >"$README_PATH" <<'EOF'
This directory stores the generated Flutter Web bundle for the admin console (塘主),
which is embedded into the Go API binary and served under `/admin`.

Do not hand-edit generated files here. Use `backend/scripts/sync_admin_dist.sh` to
rebuild and sync the admin Flutter Web bundle into this directory before compiling
`cmd/api`.
EOF
}

CURRENT_INPUT_FINGERPRINT="$(compute_input_fingerprint)"

if [[ "$FORCE_ADMIN_REBUILD" != "1" && -f "$DIST_DIR/index.html" ]]; then
	if saved="$(read_saved_fingerprint "$DIST_FINGERPRINT_PATH")"; then
		if [[ -n "$CURRENT_INPUT_FINGERPRINT" && "$saved" == "$CURRENT_INPUT_FINGERPRINT" ]]; then
			echo "reuse existing admin Web bundle (fingerprint=$CURRENT_INPUT_FINGERPRINT)"
			exit 0
		fi
	fi
fi

build_cmd=(flutter build web "$build_flag" "--base-href=$BASE_HREF")
if [[ "$WEB_USE_WASM" == "1" ]]; then
	build_cmd+=("--wasm")
fi
if [[ "$NO_TREE_SHAKE_ICONS" == "1" ]]; then
	build_cmd+=("--no-tree-shake-icons")
fi
if [[ -n "$ADMIN_API_BASE_URL" ]]; then
	build_cmd+=("--dart-define=ADMIN_API_BASE_URL=$ADMIN_API_BASE_URL")
fi
if (($# > 0)); then
	build_cmd+=("$@")
fi

pushd "$ADMIN_DIR" >/dev/null
flutter pub get
"${build_cmd[@]}"
popd >/dev/null

FRONTEND_FONT_FALLBACKS="$REPO_ROOT/frontend/web/font-fallbacks"
if [[ -d "$FRONTEND_FONT_FALLBACKS" ]]; then
	echo "copy font-fallbacks from frontend into admin build"
	rsync -a "$FRONTEND_FONT_FALLBACKS/" "$ADMIN_DIR/build/web/font-fallbacks/"
fi

sync_web_bundle "$ADMIN_DIR/build/web" "$DIST_DIR"
write_fingerprint "$DIST_DIR"
write_readme

echo "synced admin Web bundle into $DIST_DIR"
