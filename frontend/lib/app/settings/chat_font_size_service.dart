import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

enum ChatFontSizeLevel { small, medium, large }

class ChatFontSizeService extends GetxService {
  static const String prefsKey = 'chat_font_size_level';
  static bool _prefsUnavailableLogged = false;
  final Rx<ChatFontSizeLevel> _level = ChatFontSizeLevel.medium.obs;
  final RxDouble _scale = 1.0.obs;

  Future<ChatFontSizeService> init() async {
    final prefs = await _safeGetPrefs();
    _applyLevel(
      prefs != null
          ? _parseLevel(prefs.getString(prefsKey))
          : ChatFontSizeLevel.medium,
    );
    return this;
  }

  ChatFontSizeLevel get level => _level.value;
  Rx<ChatFontSizeLevel> get levelRx => _level;

  double get scale => _scale.value;
  RxDouble get scaleRx => _scale;

  String get translationKey => translationKeyForLevel(level);

  Future<void> setLevel(ChatFontSizeLevel next) async {
    if (_level.value == next) return;
    _applyLevel(next);

    final prefs = await _safeGetPrefs();
    if (prefs == null) return;
    await prefs.setString(prefsKey, _serializeLevel(next));
  }

  double scaleForLevel(ChatFontSizeLevel level) {
    switch (level) {
      case ChatFontSizeLevel.small:
        return 0.9;
      case ChatFontSizeLevel.medium:
        return 1.0;
      case ChatFontSizeLevel.large:
        return 1.12;
    }
  }

  String translationKeyForLevel(ChatFontSizeLevel level) {
    switch (level) {
      case ChatFontSizeLevel.small:
        return 'settings_font_size_small';
      case ChatFontSizeLevel.medium:
        return 'settings_font_size_medium';
      case ChatFontSizeLevel.large:
        return 'settings_font_size_large';
    }
  }

  ChatFontSizeLevel _parseLevel(String? raw) {
    switch (raw) {
      case 'small':
        return ChatFontSizeLevel.small;
      case 'large':
        return ChatFontSizeLevel.large;
      default:
        return ChatFontSizeLevel.medium;
    }
  }

  String _serializeLevel(ChatFontSizeLevel level) {
    switch (level) {
      case ChatFontSizeLevel.small:
        return 'small';
      case ChatFontSizeLevel.medium:
        return 'medium';
      case ChatFontSizeLevel.large:
        return 'large';
    }
  }

  void _applyLevel(ChatFontSizeLevel next) {
    _level.value = next;
    _scale.value = scaleForLevel(next);
  }

  Future<SharedPreferences?> _safeGetPrefs() async {
    try {
      return await SharedPreferences.getInstance();
    } on MissingPluginException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    } on PlatformException catch (e) {
      _logPrefsUnavailable(e);
      return null;
    }
  }

  void _logPrefsUnavailable(Object error) {
    if (_prefsUnavailableLogged) return;
    _prefsUnavailableLogged = true;
    debugPrint(
      'SharedPreferences unavailable, skip chat font size persistence: $error',
    );
  }
}
