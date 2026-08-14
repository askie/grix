SHELL := /bin/bash
.PHONY: help ios-local ios-online ios-ipa-online macos-local macos-online web-local web-online android-aab-online android-apk-online pub-get

FLUTTER ?= flutter

LOCAL_ADMIN_API_BASE_URL ?= http://127.0.0.1:27180
ONLINE_ADMIN_API_BASE_URL ?= https://grix.dhf.pub

LOCAL_DART_DEFINES = --dart-define=ADMIN_API_BASE_URL=$(LOCAL_ADMIN_API_BASE_URL)
ONLINE_DART_DEFINES = --dart-define=ADMIN_API_BASE_URL=$(ONLINE_ADMIN_API_BASE_URL)

help:
	@echo "admin 开发命令："
	@echo "  make ios-local        本地后端 iOS 模拟器"
	@echo "  make ios-online       线上后端 iOS 模拟器"
	@echo "  make ios-ipa-online   构建 iOS IPA（线上后端）"
	@echo "  make macos-local      本地后端 macOS"
	@echo "  make macos-online     线上后端 macOS"
	@echo "  make web-local        本地后端 Web"
	@echo "  make web-online       线上后端 Web"
	@echo "  make android-aab-online  构建 Android AAB（线上后端）"
	@echo "  make android-apk-online  构建 Android APK（线上后端）"

pub-get:
	$(FLUTTER) pub get

ios-local: pub-get
	$(FLUTTER) run -d iPhone $(LOCAL_DART_DEFINES)

ios-online: pub-get
	$(FLUTTER) run -d iPhone $(ONLINE_DART_DEFINES)

ios-ipa-online: pub-get
	$(FLUTTER) build ipa --release $(ONLINE_DART_DEFINES)

macos-local: pub-get
	$(FLUTTER) run -d macos $(LOCAL_DART_DEFINES)

macos-online: pub-get
	$(FLUTTER) run -d macos $(ONLINE_DART_DEFINES)

web-local: pub-get
	$(FLUTTER) run -d chrome $(LOCAL_DART_DEFINES)

web-online: pub-get
	$(FLUTTER) run -d chrome $(ONLINE_DART_DEFINES)

android-aab-online: pub-get
	$(FLUTTER) build appbundle --release $(ONLINE_DART_DEFINES)

android-apk-online: pub-get
	$(FLUTTER) build apk --release $(ONLINE_DART_DEFINES)
