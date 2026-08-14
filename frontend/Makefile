SHELL := /bin/bash

.PHONY: help pub-get verify-web-update-logic sync-web-font-fallbacks web-local web-local-dashmon macos-local-dashmon windows-local-dashmon web-test-online macos-test-online ios-test-online build-web-local build-web-test-online build-macos build-windows build-linux build-ios build-apk

FLUTTER ?= flutter
DASHMON ?= dashmon

# GnuWin32 make uses CreateProcess which can't resolve .bat wrappers (like dashmon.bat).
# Fall back to dart pub global run on Windows.
ifeq ($(OS),Windows_NT)
  DASHMON = dart pub global run dashmon
endif
WEB_DEVICE ?= chrome
WEB_HOSTNAME ?= 127.0.0.1
WEB_PORT ?= 34123
WEB_ALLOWED_ORIGIN ?= http://$(WEB_HOSTNAME):$(WEB_PORT)
IOS_DEVICE_ID ?=

LOCAL_API_BASE_URL ?= http://127.0.0.1:27180/v1
LOCAL_WS_URL ?= ws://127.0.0.1:27189/ws

ONLINE_API_ORIGIN ?= https://grix.dhf.pub
ONLINE_WS_ORIGIN ?= wss://grix.dhf.pub
ONLINE_API_BASE_URL ?= $(ONLINE_API_ORIGIN)/v1
ONLINE_WS_URL ?= $(ONLINE_WS_ORIGIN)/ws

# 全球区域线上端点（与 app_region_config.dart 全球默认值一致）
ONLINE_GLOBAL_API_ORIGIN ?= https://gb.grix.im
ONLINE_GLOBAL_WS_ORIGIN ?= wss://ws.grix.im
ONLINE_GLOBAL_API_BASE_URL ?= $(ONLINE_GLOBAL_API_ORIGIN)/v1
ONLINE_GLOBAL_WS_URL ?= $(ONLINE_GLOBAL_WS_ORIGIN)/ws

SENTRY_DSN ?=

# Google 登录 Web 端 OAuth Client ID（GIS client_id）；全球区网页登录用
GOOGLE_WEB_CLIENT_ID ?= 16136457990-j15mu6gvh3etnfs0c2c7vc2mjnds6ijl.apps.googleusercontent.com

# 区域端点：本地两区域全部指向本机
LOCAL_DART_DEFINES = \
	--dart-define=API_BASE_URL=$(LOCAL_API_BASE_URL) \
	--dart-define=WS_URL=$(LOCAL_WS_URL) \
	--dart-define=CN_API_URL=$(LOCAL_API_BASE_URL) \
	--dart-define=CN_WS_URL=$(LOCAL_WS_URL) \
	--dart-define=GLOBAL_API_URL=$(LOCAL_API_BASE_URL) \
	--dart-define=GLOBAL_WS_URL=$(LOCAL_WS_URL) \
	--dart-define=SENTRY_DSN=$(SENTRY_DSN) \
	--dart-define=GOOGLE_WEB_CLIENT_ID=$(GOOGLE_WEB_CLIENT_ID)

# 线上运行（本机访问线上后端）：API_BASE_URL 指向 CN，避免本机同源误判
ONLINE_DART_DEFINES = \
	--dart-define=API_BASE_URL=$(ONLINE_API_BASE_URL) \
	--dart-define=WS_URL=$(ONLINE_WS_URL) \
	--dart-define=CN_API_URL=$(ONLINE_API_BASE_URL) \
	--dart-define=CN_WS_URL=$(ONLINE_WS_URL) \
	--dart-define=GLOBAL_API_URL=$(ONLINE_GLOBAL_API_BASE_URL) \
	--dart-define=GLOBAL_WS_URL=$(ONLINE_GLOBAL_WS_URL) \
	--dart-define=SENTRY_DSN=$(SENTRY_DSN) \
	--dart-define=GOOGLE_WEB_CLIENT_ID=$(GOOGLE_WEB_CLIENT_ID)

# 线上部署构建：不设 API_BASE_URL/WS_URL，让 web 自动走同源，支持 CN 和全球区复用同一产物
ONLINE_WEB_DEPLOY_DART_DEFINES = \
	--dart-define=CN_API_URL=$(ONLINE_API_BASE_URL) \
	--dart-define=CN_WS_URL=$(ONLINE_WS_URL) \
	--dart-define=GLOBAL_API_URL=$(ONLINE_GLOBAL_API_BASE_URL) \
	--dart-define=GLOBAL_WS_URL=$(ONLINE_GLOBAL_WS_URL) \
	--dart-define=SENTRY_DSN=$(SENTRY_DSN) \
	--dart-define=GOOGLE_WEB_CLIENT_ID=$(GOOGLE_WEB_CLIENT_ID)

help:
	@printf '%s\n' \
		'make sync-web-font-fallbacks Sync local Flutter Web fallback fonts and rebuild bundled UI zh subset font' \
		'make web-local            Run Flutter Web against local backend' \
		'make web-local-dashmon    Run Dashmon Web against local backend with fixed host/port' \
		'make macos-local-dashmon  Run Dashmon macOS app against local backend' \
		'make windows-local-dashmon Run Dashmon Windows app against local backend' \
		'make web-test-online      Run Flutter Web against online backend with fixed local origin' \
		'make macos-test-online    Run Flutter macOS app against online backend' \
		'make ios-test-online      Run Flutter iPhone debug build against online backend' \
		'make build-macos           Build macOS release app' \
		'make build-windows         Build Windows release app' \
		'make build-linux           Build Linux release app' \
		'make build-ios             Build iOS release app without codesign' \
		'make build-apk             Build Android release APK' \
		'make build-web-local      Build Flutter Web for local backend' \
		'make build-web-test-online Build Flutter Web for online backend' \
		'' \
		'Variables:' \
		'  WEB_DEVICE=<flutter-web-device> Default: chrome' \
		'  WEB_HOSTNAME=<host>             Default: 127.0.0.1' \
		'  WEB_PORT=<port>                 Default: 34123' \
		'  WEB_ALLOWED_ORIGIN              Default: http://127.0.0.1:34123' \
		'  IOS_DEVICE_ID=<device-id>       Optional: override auto-detected iPhone id' \
		'  FLUTTER=<flutter-bin>           Default: flutter'

pub-get:
	$(FLUTTER) pub get

verify-web-update-logic:
	python3 scripts/verify_web_build_id_logic.py

sync-web-font-fallbacks:
	python3 scripts/sync_web_font_fallbacks.py
	python3 scripts/build_web_ui_zh_subset_font.py

web-local: pub-get
	$(FLUTTER) run -d $(WEB_DEVICE) \
		--wasm \
		$(LOCAL_DART_DEFINES)

web-local-dashmon: pub-get
	$(DASHMON) -d $(WEB_DEVICE) \
		--wasm \
		--web-hostname=$(WEB_HOSTNAME) \
		--web-port=$(WEB_PORT) \
		$(LOCAL_DART_DEFINES)

macos-local-dashmon: pub-get
	$(DASHMON) -d macos \
		$(LOCAL_DART_DEFINES)

# 单实例锁按「同一 profile」判定：若本机已装的正式版 grix.exe 在跑（同为 default
# profile），本地调试进程会在 bootstrapInstanceProfile 里抢锁失败，转而把正在跑的
# 正式版（线上）窗口带到前台后静默退出——表现为「启动命令连了线上服务器」，其实
# dart-define 早就正确指向本地，只是调试窗口根本没起来。用独立 profile 避开这个锁
# 冲突，保证调试构建真正拉起自己的新窗口。
windows-local-dashmon: export GRIX_PROFILE = devlocal
windows-local-dashmon: pub-get
	$(FLUTTER) run -d windows --no-dds \
		$(LOCAL_DART_DEFINES)

web-test-online: pub-get
	$(FLUTTER) run -d $(WEB_DEVICE) \
		--wasm \
		--web-hostname=$(WEB_HOSTNAME) \
		--web-port=$(WEB_PORT) \
		$(ONLINE_DART_DEFINES)

macos-test-online: pub-get
	$(FLUTTER) run -d macos \
		$(ONLINE_DART_DEFINES)

ios-test-online: pub-get
	@set -euo pipefail; \
	device_id="$(IOS_DEVICE_ID)"; \
	if [[ -z "$$device_id" ]]; then \
		device_id="$$( $(FLUTTER) devices --machine | /usr/bin/python3 -c 'import json, sys; devices=json.load(sys.stdin); physical=[d for d in devices if not d.get("emulator", False) and d.get("id") and (d.get("platformType") == "ios" or str(d.get("targetPlatform", "")).startswith("ios"))]; \
if not physical: print("no connected physical iPhone device found", file=sys.stderr) or sys.exit(1); \
if len(physical) > 1: print("multiple connected physical iPhone devices found: " + ", ".join("{}({})".format(d.get("name", "unknown"), d["id"]) for d in physical), file=sys.stderr) or sys.exit(2); \
print(physical[0]["id"])' )"; \
	fi; \
	echo "using ios device: $$device_id"; \
	$(FLUTTER) run -d "$$device_id" \
		$(ONLINE_DART_DEFINES)

build-web-local: pub-get verify-web-update-logic
	$(FLUTTER) build web \
		--wasm \
		$(LOCAL_DART_DEFINES)
	@web_build_id="$$(date -u +%Y%m%d%H%M%S)-$$(git rev-parse --short HEAD 2>/dev/null || echo nogit)"; \
	python3 -c 'import json,sys; p="build/web/version.json"; d=json.load(open(p,"r",encoding="utf-8")); d["web_build_id"]=sys.argv[1]; open(p,"w",encoding="utf-8").write(json.dumps(d,separators=(",",":")))' "$$web_build_id"; \
	echo "web_build_id=$$web_build_id"

build-web-test-online: pub-get verify-web-update-logic
	$(FLUTTER) build web \
		--wasm \
		$(ONLINE_WEB_DEPLOY_DART_DEFINES)
	@web_build_id="$$(date -u +%Y%m%d%H%M%S)-$$(git rev-parse --short HEAD 2>/dev/null || echo nogit)"; \
	python3 -c 'import json,sys; p="build/web/version.json"; d=json.load(open(p,"r",encoding="utf-8")); d["web_build_id"]=sys.argv[1]; open(p,"w",encoding="utf-8").write(json.dumps(d,separators=(",",":")))' "$$web_build_id"; \
	echo "web_build_id=$$web_build_id"

build-macos: pub-get
	$(FLUTTER) build macos --release \
		$(ONLINE_DART_DEFINES)

build-windows: pub-get
	$(FLUTTER) build windows --release \
		$(ONLINE_DART_DEFINES)

build-linux: pub-get
	$(FLUTTER) build linux --release \
		$(ONLINE_DART_DEFINES)

build-ios: pub-get
	$(FLUTTER) build ios --release --no-codesign \
		$(ONLINE_DART_DEFINES)

build-apk: pub-get
	$(FLUTTER) build apk --release \
		$(ONLINE_DART_DEFINES)

# Fallback for any target with no matching rule: show the error plus the full command list.
%:
	@echo "make: *** No rule to make target '$@'." >&2
	@echo "" >&2
	@"$(MAKE)" --no-print-directory help
	@exit 2
