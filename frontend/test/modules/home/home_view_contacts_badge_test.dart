import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_category_service.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/home/bindings/home_binding.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/controllers/home_controller.dart';
import 'package:grix/modules/home/home_view.dart';
import 'package:grix/shared/widgets/app_icon.dart';

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

class _FakeAgentService extends AgentService {
  int loadCalls = 0;

  @override
  Future<void> loadAgents({String? categoryId}) async {
    loadCalls++;
  }
}

class _FakeAgentCategoryService extends AgentCategoryService {
  int loadCalls = 0;

  @override
  Future<void> loadCategories() async {
    loadCalls++;
  }

  @override
  Future<void> restoreCachedCategories() async {}

  @override
  Future<void> syncCategoriesFromRemote() async {}
}

Map<String, dynamic> _friendReq({
  required String id,
  required String fromUserId,
  required int status,
}) {
  return <String, dynamic>{
    'id': id,
    'from_user_id': fromUserId,
    'username': 'user_$fromUserId',
    'nickname': 'User $fromUserId',
    'avatar_url': '',
    'message': 'hi',
    'status': status,
    'created_at': '2026-03-04T10:00:00Z',
  };
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeFriendService friendService;
  late _FakeAgentService agentService;
  late _FakeAgentCategoryService agentCategoryService;

  setUp(() {
    Get.testMode = true;
    Get.reset();

    friendService = _FakeFriendService();
    agentService = _FakeAgentService();
    agentCategoryService = _FakeAgentCategoryService();
    Get.put<ImService>(_FakeImService());
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FriendService>(friendService);
    Get.put<AgentService>(agentService);
    Get.put<AgentCategoryService>(agentCategoryService);
    HomeBinding().dependencies();
  });

  tearDown(() async {
    Get.reset();
  });

  testWidgets(
      'contacts tab badge updates when receiving pending friend requests',
      (WidgetTester tester) async {
    tester.view.physicalSize = const Size(420, 920);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      const GetMaterialApp(
        home: HomeView(),
      ),
    );
    await tester.pumpAndSettle();

    final bottomNav = find.byType(BottomNavigationBar);

    expect(
      find.descendant(of: bottomNav, matching: find.text('1')),
      findsNothing,
    );

    friendService.applyRealtimeEvent(<String, dynamic>{
      'event': 'friend_request_received',
      'request': _friendReq(id: '9001', fromUserId: '2001', status: 0),
    });
    await tester.pump();

    expect(
      find.descendant(of: bottomNav, matching: find.text('1')),
      findsOneWidget,
    );

    friendService.applyRealtimeEvent(<String, dynamic>{
      'event': 'friend_request_received',
      'request': _friendReq(id: '9002', fromUserId: '2002', status: 0),
    });
    await tester.pump();

    expect(
      find.descendant(of: bottomNav, matching: find.text('2')),
      findsOneWidget,
    );

    friendService.applyRealtimeEvent(<String, dynamic>{
      'event': 'friend_request_handled',
      'request_id': '9001',
      'status': 1,
    });
    await tester.pump();

    expect(
      find.descendant(of: bottomNav, matching: find.text('1')),
      findsOneWidget,
    );
  });

  testWidgets('bottom navigation uses svg app icons on web-safe path',
      (WidgetTester tester) async {
    tester.view.physicalSize = const Size(420, 920);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      const GetMaterialApp(
        home: HomeView(),
      ),
    );
    await tester.pumpAndSettle();

    final bottomNav = find.byType(BottomNavigationBar);

    expect(
      find.descendant(of: bottomNav, matching: find.byType(AppIcon)),
      findsNWidgets(5),
    );
  });

  testWidgets('messages tab badge excludes muted session unread count',
      (WidgetTester tester) async {
    final imService = Get.find<ImService>();
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'session-visible',
        type: 'private',
        updatedAt: 2000,
        unreadCount: 3,
        lastMessage: 'visible unread',
        lastMessageTime: 2000,
      ),
      SessionModel(
        sessionId: 'session-muted',
        type: 'group',
        updatedAt: 3000,
        unreadCount: 5,
        lastMessage: 'muted unread',
        lastMessageTime: 3000,
        isMuted: true,
      ),
    ]);

    tester.view.physicalSize = const Size(420, 920);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      const GetMaterialApp(
        home: HomeView(),
      ),
    );
    await tester.pumpAndSettle();

    final bottomNav = find.byType(BottomNavigationBar);

    expect(
      find.descendant(of: bottomNav, matching: find.text('3')),
      findsOneWidget,
    );
    expect(
      find.descendant(of: bottomNav, matching: find.text('8')),
      findsNothing,
    );

    tester.view.physicalSize = const Size(420, 920);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(const SizedBox.shrink());
    Get.delete<ConversationsController>(force: true);
    Get.delete<HomeController>(force: true);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 2500));
  });

  testWidgets('agents tab loads data only after tab is opened',
      (WidgetTester tester) async {
    tester.view.physicalSize = const Size(420, 920);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.reset);
    await tester.pumpWidget(
      const GetMaterialApp(
        home: HomeView(),
      ),
    );
    await tester.pumpAndSettle();

    expect(agentService.loadCalls, 0);

    await tester.tap(find.text('nav_ai'));
    await tester.pumpAndSettle();

    expect(agentService.loadCalls, 1);
  });
}
