import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/settings/agent_toolbar_visibility_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('AgentToolbarVisibilityService', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    tearDown(() async {
      if (Get.isRegistered<AgentToolbarVisibilityService>()) {
        await Get.delete<AgentToolbarVisibilityService>();
      }
    });

    test('defaults to visible when preference is absent', () async {
      final service = await AgentToolbarVisibilityService().init();

      expect(service.visible, isTrue);
      expect(service.visibleRx.value, isTrue);
    });

    test('loads persisted hidden value', () async {
      SharedPreferences.setMockInitialValues({
        AgentToolbarVisibilityService.prefsKey: false,
      });

      final service = await AgentToolbarVisibilityService().init();

      expect(service.visible, isFalse);
    });

    test('toggle persists value and survives re-init', () async {
      final service = await AgentToolbarVisibilityService().init();
      await service.toggle();

      expect(service.visible, isFalse);
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getBool(AgentToolbarVisibilityService.prefsKey), isFalse);

      final reloaded = await AgentToolbarVisibilityService().init();
      expect(reloaded.visible, isFalse);
    });

    test('setVisible updates state and persistence', () async {
      final service = await AgentToolbarVisibilityService().init();
      await service.setVisible(false);

      expect(service.visible, isFalse);
      final prefs = await SharedPreferences.getInstance();
      expect(prefs.getBool(AgentToolbarVisibilityService.prefsKey), isFalse);
    });

    test('registers and resolves via Get (app_initializer contract)', () async {
      await Get.putAsync<AgentToolbarVisibilityService>(
        () => AgentToolbarVisibilityService().init(),
      );

      expect(Get.isRegistered<AgentToolbarVisibilityService>(), isTrue);
      expect(Get.find<AgentToolbarVisibilityService>().visible, isTrue);
    });
  });
}
