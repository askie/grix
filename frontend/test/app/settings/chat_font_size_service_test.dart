import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/settings/chat_font_size_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('ChatFontSizeService', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('defaults to medium when preference is absent', () async {
      final service = await ChatFontSizeService().init();

      expect(service.level, ChatFontSizeLevel.medium);
      expect(service.levelRx.value, ChatFontSizeLevel.medium);
      expect(service.scale, 1.0);
      expect(service.scaleRx.value, 1.0);
      expect(service.translationKey, 'settings_font_size_medium');
    });

    test('loads persisted large level', () async {
      SharedPreferences.setMockInitialValues({
        ChatFontSizeService.prefsKey: 'large',
      });

      final service = await ChatFontSizeService().init();

      expect(service.level, ChatFontSizeLevel.large);
      expect(service.levelRx.value, ChatFontSizeLevel.large);
      expect(service.scale, 1.12);
      expect(service.scaleRx.value, 1.12);
      expect(service.translationKey, 'settings_font_size_large');
    });

    test('setLevel persists value', () async {
      final service = await ChatFontSizeService().init();
      await service.setLevel(ChatFontSizeLevel.small);

      final prefs = await SharedPreferences.getInstance();
      expect(
        prefs.getString(ChatFontSizeService.prefsKey),
        'small',
      );
      expect(service.level, ChatFontSizeLevel.small);
      expect(service.levelRx.value, ChatFontSizeLevel.small);
      expect(service.scale, 0.9);
      expect(service.scaleRx.value, 0.9);
    });
  });
}
