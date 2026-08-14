import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/settings/agent_toolbar_visibility_service.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:get/get.dart';
import 'package:grix/app/routes/app_routes.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/controllers/home_controller.dart';
import 'package:grix/modules/home/services/home_tab_url_sync.dart';
import 'package:shared_preferences/shared_preferences.dart';

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
    User(id: '1001', username: 'tester', nickname: 'Tester'),
  );

  @override
  bool get isLoggedIn => _loggedIn.value;

  @override
  User? get user => _userState.value;

  @override
  String? get userId => _userState.value?.id;
}

class _FakeFriendService extends FriendService {}

class _FakeHomeTabUrlSync implements HomeTabUrlSync {
  _FakeHomeTabUrlSync({required String initialPath})
    : _currentPath = initialPath;

  final StreamController<String> _pathController =
      StreamController<String>.broadcast();
  String _currentPath;
  int pushCalls = 0;
  int replaceCalls = 0;

  @override
  String get currentPath => _currentPath;

  @override
  Stream<String> get onPathChanged => _pathController.stream;

  @override
  void pushPath(String path) {
    pushCalls++;
    _currentPath = path;
    _pathController.add(path);
  }

  @override
  void replacePath(String path) {
    replaceCalls++;
    _currentPath = path;
  }

  void simulateExternalPathChange(String path) {
    _currentPath = path;
    _pathController.add(path);
  }

  @override
  void dispose() {
    _pathController.close();
  }
}

void main() {
  late _FakeFriendService friendService;
  late _FakeHomeTabUrlSync urlSync;
  late HomeController controller;

  setUp(() {
    Get.testMode = true;
    Get.reset();

    friendService = _FakeFriendService();

    Get.put<ImService>(_FakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FriendService>(friendService);

    urlSync = _FakeHomeTabUrlSync(initialPath: AppRoutes.home);
    controller = Get.put(HomeController(urlSync: urlSync));
  });

  tearDown(() {
    Get.reset();
  });

  test('changePage updates current tab index', () {
    controller.changePage(1);
    expect(controller.currentIndex.value, 1);
    expect(urlSync.currentPath, AppRoutes.homeAgents);

    controller.changePage(2);
    expect(controller.currentIndex.value, 2);
    expect(urlSync.currentPath, AppRoutes.homeEggsPond);

    controller.changePage(3);
    expect(controller.currentIndex.value, 3);
    expect(urlSync.currentPath, AppRoutes.homeContacts);
    expect(urlSync.pushCalls, 0);
    expect(urlSync.replaceCalls, 3);
  });

  test('home tab navigation replaces current history entry', () {
    controller.changePage(HomeTab.settings.index);

    expect(controller.currentIndex.value, HomeTab.settings.index);
    expect(urlSync.currentPath, AppRoutes.homeSettings);
    expect(urlSync.pushCalls, 0);
    expect(urlSync.replaceCalls, 1);
  });

  test('initial tab index follows current route path', () {
    final urlSync = _FakeHomeTabUrlSync(initialPath: AppRoutes.homeSettings);
    final controller = HomeController(urlSync: urlSync);

    controller.onInit();

    expect(controller.currentIndex.value, HomeTab.settings.index);
    expect(urlSync.replaceCalls, 0);
    controller.onClose();
  });

  test('invalid initial home path is normalized without adding history', () {
    final urlSync = _FakeHomeTabUrlSync(initialPath: AppRoutes.splash);
    final controller = HomeController(urlSync: urlSync);

    controller.onInit();

    expect(controller.currentIndex.value, HomeTab.conversations.index);
    expect(urlSync.currentPath, AppRoutes.home);
    expect(urlSync.replaceCalls, 1);
    controller.onClose();
  });

  test('path changes from browser history update current tab index', () async {
    expect(controller.currentIndex.value, HomeTab.conversations.index);

    urlSync.simulateExternalPathChange(AppRoutes.homeSettings);
    await Future<void>.delayed(Duration.zero);

    expect(controller.currentIndex.value, HomeTab.settings.index);
  });

  test('pending friend request count includes only status=0', () {
    friendService.friendList.add(
      FriendItem(
        id: 'f1',
        userId: 'u1',
        username: 'alice',
        nickname: 'Alice',
        remarkName: '',
        avatarUrl: '',
      ),
    );
    friendService.friendRequests.add(
      FriendRequestItem(
        id: 'r1',
        fromUserId: 'u2',
        username: 'bob',
        nickname: 'Bob',
        avatarUrl: '',
        message: '',
        status: 0,
        createdAt: '2026-03-08T00:00:00Z',
      ),
    );

    friendService.friendRequests.add(
      FriendRequestItem(
        id: 'r2',
        fromUserId: 'u3',
        username: 'carol',
        nickname: 'Carol',
        avatarUrl: '',
        message: '',
        status: 1,
        createdAt: '2026-03-08T00:00:00Z',
      ),
    );

    expect(controller.pendingFriendRequestCount, 1);
  });

  test('retapping messages tab emits scroll signal when unread exists', () {
    final imService = Get.find<ImService>();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 's-1',
        title: 'Unread',
        type: 'private',
        updatedAt: 100,
        unreadCount: 2,
        lastMessage: 'hello',
        lastMessageTime: 100,
      ),
    ]);

    expect(controller.messagesTabRetapTick.value, 0);

    controller.handleTabTap(HomeTab.conversations.index);

    expect(controller.currentIndex.value, HomeTab.conversations.index);
    expect(controller.messagesTabRetapTick.value, 1);
    expect(urlSync.currentPath, AppRoutes.home);
    expect(urlSync.pushCalls, 0);
  });

  test('retapping messages tab does not emit scroll signal without unread', () {
    expect(controller.messagesTabRetapTick.value, 0);

    controller.handleTabTap(HomeTab.conversations.index);

    expect(controller.messagesTabRetapTick.value, 0);
  });

  test('loads persisted hidden toolbar visibility on init', () async {
    SharedPreferences.setMockInitialValues({
      AgentToolbarVisibilityService.prefsKey: false,
    });
    await Get.putAsync<AgentToolbarVisibilityService>(
      () => AgentToolbarVisibilityService().init(),
    );
    final urlSync = _FakeHomeTabUrlSync(initialPath: AppRoutes.home);
    final controller = HomeController(urlSync: urlSync);

    controller.onInit();

    expect(controller.agentToolbarVisible.value, isFalse);
    controller.onClose();
    await Get.delete<AgentToolbarVisibilityService>();
  });

  test('keeps default toolbar visibility when service is not registered', () {
    final urlSync = _FakeHomeTabUrlSync(initialPath: AppRoutes.home);
    final controller = HomeController(urlSync: urlSync);

    controller.onInit();

    expect(controller.agentToolbarVisible.value, isTrue);
    expect(() => controller.toggleAgentToolbarVisibility(), returnsNormally);
    expect(controller.agentToolbarVisible.value, isFalse);
    controller.onClose();
  });
}
