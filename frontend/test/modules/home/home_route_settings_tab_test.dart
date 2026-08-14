import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/app/settings/theme_preference_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/bindings/home_binding.dart';
import 'package:grix/modules/home/home_view.dart';
import 'package:grix/modules/profile/profile_view.dart';

class _FakeImService extends ImService {
  final RxBool _connected = true.obs;

  @override
  bool get isConnected => _connected.value;

  @override
  void connect(String wsUrl) {}
}

class _FakeAuthService extends AuthService {
  final RxBool _loggedIn = true.obs;
  final Rxn<User> _userState = Rxn<User>(
    User(
      id: '1001',
      username: 'tester',
      email: 'tester@example.com',
      nickname: 'Tester',
    ),
  );

  @override
  bool get isLoggedIn => _loggedIn.value;

  @override
  User? get user => _userState.value;

  @override
  String? get userId => _userState.value?.id;
}

class _FakeFriendService extends FriendService {
  @override
  Future<void> loadFriendList() async {}

  @override
  Future<void> loadFriendRequests() async {}
}

class _FakeAgentService extends AgentService {}

class _FakeThemePreferenceService extends ThemePreferenceService {
  @override
  Future<void> toggle() async {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();

    Get.put<ImService>(_FakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FriendService>(_FakeFriendService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<ThemePreferenceService>(_FakeThemePreferenceService());
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets(
    'settings tab route renders through route binding without missing avatar cropper service',
    (WidgetTester tester) async {
      await tester.pumpWidget(
        GetMaterialApp(
          initialRoute: AppRoutes.homeSettings,
          getPages: [
            GetPage(
              name: AppRoutes.home,
              page: () => const HomeView(),
              binding: HomeBinding(),
            ),
            GetPage(
              name: AppRoutes.homeSettings,
              page: () => const HomeView(),
              binding: HomeBinding(),
            ),
          ],
        ),
      );
      await tester.pumpAndSettle();

      expect(find.byType(ProfileView), findsOneWidget);
      expect(tester.takeException(), isNull);
    },
  );
}
