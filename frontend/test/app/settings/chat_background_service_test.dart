import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/settings/chat_background_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('ChatBackgroundService', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('defaults to built-in chat background color', () async {
      final service =
          await ChatBackgroundService(userIdResolver: () => 'user_a').init();

      expect(service.color, ChatBackgroundStyle.defaultColor);
      expect(service.hasImage, isFalse);
      expect(service.imageUrl, isEmpty);
    });

    test('persists selected color for current user', () async {
      final service =
          await ChatBackgroundService(userIdResolver: () => 'user_a').init();

      await service.setColor(const Color(0xFFE3F2FD));

      final restored =
          await ChatBackgroundService(userIdResolver: () => 'user_a').init();
      expect(restored.color, const Color(0xFFE3F2FD));
      expect(restored.hasImage, isFalse);
    });

    test('stores chat background independently per user', () async {
      final userA =
          await ChatBackgroundService(userIdResolver: () => 'user_a').init();
      final userB =
          await ChatBackgroundService(userIdResolver: () => 'user_b').init();

      await userA.setColor(const Color(0xFFFFF3E0));
      await userB.setColor(const Color(0xFFEDE7F6));

      final restoredA =
          await ChatBackgroundService(userIdResolver: () => 'user_a').init();
      final restoredB =
          await ChatBackgroundService(userIdResolver: () => 'user_b').init();

      expect(restoredA.color, const Color(0xFFFFF3E0));
      expect(restoredB.color, const Color(0xFFEDE7F6));
    });

    test('switches background style when current user changes', () async {
      var currentUserId = 'user_a';
      final service = await ChatBackgroundService(
        userIdResolver: () => currentUserId,
      ).init();

      await service.setColor(const Color(0xFFE8F5E9));
      expect(service.color, const Color(0xFFE8F5E9));

      currentUserId = 'user_b';
      service.ensureSyncedWithCurrentUser();
      await Future<void>.delayed(Duration.zero);
      expect(service.color, ChatBackgroundStyle.defaultColor);

      await service.setColor(const Color(0xFFFFEBEE));
      expect(service.color, const Color(0xFFFFEBEE));

      currentUserId = 'user_a';
      service.ensureSyncedWithCurrentUser();
      await Future<void>.delayed(Duration.zero);
      expect(service.color, const Color(0xFFE8F5E9));
    });

    test('setColor clears selected background image', () async {
      final service =
          await ChatBackgroundService(userIdResolver: () => 'user_a').init();

      await service.setImageUrl('https://example.com/bg.png');
      expect(service.hasImage, isTrue);

      await service.setColor(const Color(0xFFF3E5F5));
      expect(service.hasImage, isFalse);
      expect(service.imageUrl, isEmpty);
      expect(service.color, const Color(0xFFF3E5F5));
    });
  });
}
