import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/settings/chat_thinking_collapse_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('ChatThinkingCollapseService', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('defaults to expanded when preference is absent', () async {
      final service = await ChatThinkingCollapseService().init();

      expect(service.collapsed, isFalse);
      expect(service.collapsedRx.value, isFalse);
    });

    test('loads persisted collapsed value', () async {
      SharedPreferences.setMockInitialValues({
        ChatThinkingCollapseService.prefsKey: true,
      });

      final service = await ChatThinkingCollapseService().init();

      expect(service.collapsed, isTrue);
    });

    test('toggle persists value and survives re-init', () async {
      final service = await ChatThinkingCollapseService().init();
      await service.toggle();

      expect(service.collapsed, isTrue);
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getBool(ChatThinkingCollapseService.prefsKey), isTrue);

      final reloaded = await ChatThinkingCollapseService().init();
      expect(reloaded.collapsed, isTrue);
    });

    test('setCollapsed updates state and persistence', () async {
      final service = await ChatThinkingCollapseService().init();
      await service.setCollapsed(true);

      expect(service.collapsed, isTrue);
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getBool(ChatThinkingCollapseService.prefsKey), isTrue);
    });

    test('registers and resolves via Get (app_initializer contract)', () async {
      await Get.putAsync<ChatThinkingCollapseService>(
        () => ChatThinkingCollapseService().init(),
      );

      expect(Get.isRegistered<ChatThinkingCollapseService>(), isTrue);
      expect(Get.find<ChatThinkingCollapseService>().collapsed, isFalse);

      await Get.delete<ChatThinkingCollapseService>();
    });
  });
}
