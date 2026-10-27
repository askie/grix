import 'dart:io' show File, Platform;
import 'dart:ui' as ui;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/conversation_summary_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/home/controllers/contacts_controller.dart';
import 'package:grix/modules/home/controllers/conversations_controller.dart';
import 'package:grix/modules/home/conversations_view.dart';
import 'package:grix/modules/home/services/friend_qr_flow_service.dart';
import 'package:grix/shared/utils/chat_draft_index.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Tolerates minor antialiasing diffs across platforms when matching goldens.
class _TolerantComparator extends GoldenFileComparator {
  _TolerantComparator(this.basedir, {this.threshold = 0.02});

  final Uri basedir;
  final double threshold;

  @override
  Future<bool> compare(Uint8List imageBytes, Uri golden) async {
    final goldenFile = File.fromUri(basedir.resolve(golden.path));
    if (!goldenFile.existsSync()) {
      throw TestFailure('Golden file not found: ${goldenFile.path}');
    }
    final goldenBytes = goldenFile.readAsBytesSync();
    final diff = await _diffRatio(imageBytes, goldenBytes);
    return diff <= threshold;
  }

  @override
  Future<void> update(Uri golden, Uint8List imageBytes) async {
    final goldenFile = File.fromUri(basedir.resolve(golden.path));
    goldenFile.parent.createSync(recursive: true);
    goldenFile.writeAsBytesSync(imageBytes);
  }

  Future<double> _diffRatio(Uint8List a, Uint8List b) async {
    final imgA = await _decode(a);
    final imgB = await _decode(b);
    if (imgA.width != imgB.width || imgA.height != imgB.height) return 1.0;
    final bdA = await imgA.toByteData(format: ui.ImageByteFormat.rawRgba);
    final bdB = await imgB.toByteData(format: ui.ImageByteFormat.rawRgba);
    if (bdA == null || bdB == null) return 1.0;
    final total = bdA.lengthInBytes ~/ 4;
    int diffCount = 0;
    for (var i = 0; i < bdA.lengthInBytes; i += 4) {
      if (bdA.getUint32(i) != bdB.getUint32(i)) diffCount++;
    }
    imgA.dispose();
    imgB.dispose();
    return diffCount / total;
  }

  Future<ui.Image> _decode(Uint8List bytes) async {
    final codec = await ui.instantiateImageCodec(bytes);
    final frame = await codec.getNextFrame();
    codec.dispose();
    return frame.image;
  }
}

class _FakeImService extends ImService {
  final RxBool _connected = true.obs;

  @override
  bool get isConnected => _connected.value;

  @override
  void connect(String wsUrl) {}

  @override
  void enterSession(String sessionId, {Future<void>? renderGate}) {}

  @override
  void leaveSession([String? explicitSessionId]) {}

  @override
  Future<void> loadSessions({
    bool refreshFromServer = true,
    bool backfillMissingPeerIdentities = true,
  }) async {}

  @override
  Future<void> refreshSessionsNow() async {}

  @override
  Future<void> refreshSessionsWindowNow() async {}

  @override
  Future<void> refreshSessionsIfStale({
    Duration maxAge = const Duration(seconds: 45),
  }) async {}

  @override
  Future<bool> loadMoreSessionWindowIfNeeded({bool force = false}) async {
    return false;
  }

  @override
  Future<void> reconcilePinsFromConversationSummaries(
    List<ConversationSummaryModel> items, {
    required bool hasMore,
  }) async {}
}

class _FakeAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';

  @override
  String? get token => 'test_access_token';

  @override
  bool hasUsableAccessToken({Duration minRemaining = Duration.zero}) => true;

  @override
  Future<TokenRefreshStatus> ensureTokenFreshStatus({
    bool force = false,
    Duration threshold = const Duration(minutes: 5),
  }) async => TokenRefreshStatus.ready;
}

class _FakeFriendService extends FriendService {
  @override
  Future<void> loadFriendList() async {}

  @override
  Future<void> loadFriendRequests() async {}

  @override
  Future<String?> fetchUserProfile(String userId) async => null;
}

class _FakeAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class _FakeSessionService extends SessionService {
  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    return const SessionDetailResult(data: null);
  }
}

class _FakeOssService extends OssService {}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUpAll(() async {
    if (Platform.isLinux) {
      goldenFileComparator = _TolerantComparator(
        Uri.directory('test/modules/home/'),
        threshold: 0.02,
      );
    }
    // Widget tests default to the box-glyph test font; load real fonts so the
    // golden actually shows the site domains.
    final roboto = File('../admin/assets/fonts/Roboto-Regular.ttf');
    if (roboto.existsSync()) {
      final bytes = roboto.readAsBytesSync();
      final loader = FontLoader('Roboto')
        ..addFont(
          Future<ByteData>.value(
            ByteData.view(bytes.buffer, bytes.offsetInBytes, bytes.length),
          ),
        );
      await loader.load();
    }
    final flutterRoot = Platform.environment['FLUTTER_ROOT'] ?? '';
    final materialIcons = File(
      '$flutterRoot/bin/cache/artifacts/material_fonts/MaterialIcons-Regular.otf',
    );
    if (materialIcons.existsSync()) {
      final bytes = materialIcons.readAsBytesSync();
      final loader = FontLoader('MaterialIcons')
        ..addFont(
          Future<ByteData>.value(
            ByteData.view(bytes.buffer, bytes.offsetInBytes, bytes.length),
          ),
        );
      await loader.load();
    }
  });

  late _FakeImService imService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    ChatDraftIndex.resetForTest();
    SharedPreferences.setMockInitialValues({});
    UserImageCacheManager.setDisabledForTest(true);
    ConversationsController.useConversationListApiForTest = false;
    imService = _FakeImService();
    Get.put<ImService>(imService);
    Get.put<AuthService>(_FakeAuthService());
    Get.put<FriendService>(_FakeFriendService());
    Get.put<AgentService>(_FakeAgentService());
    Get.put<SessionService>(_FakeSessionService());
    Get.put<OssService>(_FakeOssService());
    Get.put<FriendQrFlowService>(FriendQrFlowService());
    Get.put(ContactsController());
  });

  tearDown(() {
    ConversationsController.useConversationListApiForTest = null;
    ChatDraftIndex.resetForTest();
    UserImageCacheManager.setDisabledForTest(false);
    Get.reset();
  });

  testWidgets('visitor sessions fold into two site groups plus one fallback', (
    WidgetTester tester,
  ) async {
    tester.view.physicalSize = const Size(780, 1400);
    tester.view.devicePixelRatio = 2;
    addTearDown(tester.view.reset);

    const now = 1790000000000;
    imService.sessions.assignAll([
      SessionModel(
        sessionId: 'visitor-a1',
        title: 'Visitor',
        type: 'private',
        updatedAt: now,
        isVisitor: true,
        visitorSiteId: '7101',
        visitorSiteDomain: 'shop-a.example.com',
        lastMessage: 'Where is my order?',
        lastMessageTime: now,
        unreadCount: 2,
      ),
      SessionModel(
        sessionId: 'visitor-b1',
        title: 'Visitor',
        type: 'private',
        updatedAt: now - 60000,
        isVisitor: true,
        visitorSiteId: '7102',
        visitorSiteDomain: 'shop-b.example.com',
        lastMessage: 'Do you ship to Canada?',
        lastMessageTime: now - 60000,
        unreadCount: 1,
      ),
      SessionModel(
        sessionId: 'visitor-unknown',
        title: 'Visitor',
        type: 'private',
        updatedAt: now - 120000,
        isVisitor: true,
        lastMessage: 'Hello from an old widget',
        lastMessageTime: now - 120000,
      ),
    ]);

    Get.put(ConversationsController());

    final baseTheme = AppTheme.lightTheme;
    await tester.pumpWidget(
      GetMaterialApp(
        theme: baseTheme.copyWith(
          textTheme: baseTheme.textTheme.apply(fontFamily: 'Roboto'),
        ),
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        fallbackLocale: const Locale('en', 'US'),
        home: const ConversationsView(),
      ),
    );
    await tester.pumpAndSettle();
    await tester.pump(const Duration(milliseconds: 300));

    final controller = Get.find<ConversationsController>();
    expect(controller.groupedSessions.map((i) => i.groupKey).toSet(), {
      'visitor:site:7101',
      'visitor:site:7102',
      'visitor:group',
    });
    expect(find.text('shop-a.example.com'), findsOneWidget);
    expect(find.text('shop-b.example.com'), findsOneWidget);

    await expectLater(
      find.byType(ConversationsView),
      matchesGoldenFile('goldens/conversations_visitor_sites.png'),
    );
  });
}
