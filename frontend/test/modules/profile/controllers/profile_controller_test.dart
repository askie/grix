import 'dart:async';
import 'dart:collection';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/settings/theme_preference_service.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/profile/controllers/profile_controller.dart';
import 'package:grix/modules/profile/services/avatar_cropper_service.dart';

class _FakeAuthService extends AuthService {
  User? currentUser;
  final Queue<Future<ServiceResult<void>>> _profileResponses =
      Queue<Future<ServiceResult<void>>>();
  final Queue<Future<ServiceResult<void>>> _usernameResponses =
      Queue<Future<ServiceResult<void>>>();

  @override
  User? get user => currentUser;

  void enqueueProfileResponse(Future<ServiceResult<void>> response) {
    _profileResponses.addLast(response);
  }

  void enqueueUsernameResponse(Future<ServiceResult<void>> response) {
    _usernameResponses.addLast(response);
  }

  @override
  Future<ServiceResult<void>> updateProfile({
    required String nickname,
    required String introduction,
  }) async {
    final response = _profileResponses.isEmpty
        ? ServiceResult<void>.success()
        : await _profileResponses.removeFirst();
    if (response.ok && currentUser != null) {
      final user = currentUser!;
      currentUser = User(
        id: user.id,
        username: user.username,
        email: user.email,
        nickname: nickname,
        introduction: introduction,
        avatarUrl: user.avatarUrl,
        usernameModified: user.usernameModified,
      );
    }
    return response;
  }

  @override
  Future<ServiceResult<void>> updateUsername({required String username}) async {
    final response = _usernameResponses.isEmpty
        ? ServiceResult<void>.success()
        : await _usernameResponses.removeFirst();
    if (response.ok && currentUser != null) {
      final user = currentUser!;
      currentUser = User(
        id: user.id,
        username: username,
        email: user.email,
        nickname: user.nickname,
        introduction: user.introduction,
        avatarUrl: user.avatarUrl,
        usernameModified: true,
      );
    }
    return response;
  }
}

class _FakeImService extends ImService {}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuthService authService;
  late ProfileController controller;
  late List<({String message, bool isError})> toastMessages;

  Future<void> pumpShell(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        fallbackLocale: const Locale('en', 'US'),
        home: const Scaffold(body: SizedBox.shrink()),
      ),
    );
    await tester.pump();
  }

  Future<void> openDialog(WidgetTester tester) async {
    unawaited(controller.showEditProfileDialog());
    await tester.pumpAndSettle();
    expect(find.byType(AlertDialog), findsOneWidget);
  }

  setUp(() {
    Get.testMode = true;
    Get.reset();
    authService = _FakeAuthService()
      ..currentUser = User(
        id: 'user-1',
        username: 'old_name',
        email: 'user@example.com',
        nickname: '旧昵称',
        introduction: '老介绍',
      );
    toastMessages = <({String message, bool isError})>[];

    Get.put<AuthService>(authService);
    Get.put<ImService>(_FakeImService());
    Get.put<AvatarCropperService>(AvatarCropperService());
    Get.put<ThemePreferenceService>(ThemePreferenceService());
    controller = Get.put(
      ProfileController(
        showToast: (message, {isError = true}) {
          toastMessages.add((message: message, isError: isError));
        },
      ),
    );
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('shows partial success toast when username update fails', (
    tester,
  ) async {
    authService.enqueueProfileResponse(
      Future<ServiceResult<void>>.value(ServiceResult<void>.success()),
    );
    authService.enqueueUsernameResponse(
      Future<ServiceResult<void>>.value(
        ServiceResult<void>.failure(message: '账号名称已存在'),
      ),
    );

    await pumpShell(tester);
    await openDialog(tester);

    await tester.enterText(find.byType(TextField).at(0), '新昵称');
    await tester.enterText(find.byType(TextField).at(2), 'new_name');
    await tester.tap(find.widgetWithText(ElevatedButton, '保存'));
    await tester.pump();

    expect(toastMessages, hasLength(1));
    expect(toastMessages.single.message, '昵称已保存，账号名称已存在');
    expect(toastMessages.single.isError, isTrue);
    expect(find.byType(AlertDialog), findsOneWidget);
    expect(authService.user?.nickname, '新昵称');
    expect(authService.user?.introduction, '老介绍');
    expect(authService.user?.username, 'old_name');

    final saveButton = tester.widget<ElevatedButton>(
      find.widgetWithText(ElevatedButton, '保存'),
    );
    expect(saveButton.onPressed, isNotNull);
  });

  testWidgets('maps unauthorized save failure to session expired message', (
    tester,
  ) async {
    authService.enqueueProfileResponse(
      Future<ServiceResult<void>>.value(
        ServiceResult<void>.failure(
          message: '认证失败，请检查账号或密码',
          code: 401,
          httpStatus: 401,
        ),
      ),
    );

    await pumpShell(tester);
    await openDialog(tester);

    await tester.enterText(find.byType(TextField).at(0), '新昵称');
    await tester.tap(find.widgetWithText(ElevatedButton, '保存'));
    await tester.pump();

    expect(toastMessages, hasLength(1));
    expect(toastMessages.single.message, '登录状态已失效，请重新登录后重试');
    expect(find.byType(AlertDialog), findsOneWidget);
  });

  testWidgets('re-enables actions after delayed save failure', (tester) async {
    final profileCompleter = Completer<ServiceResult<void>>();
    authService.enqueueProfileResponse(profileCompleter.future);

    await pumpShell(tester);
    await openDialog(tester);

    await tester.enterText(find.byType(TextField).at(0), '新昵称');
    await tester.tap(find.widgetWithText(ElevatedButton, '保存'));
    await tester.pump();

    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    final savingButton = tester.widget<ElevatedButton>(
      find.byType(ElevatedButton),
    );
    final cancelButton = tester.widget<TextButton>(find.byType(TextButton));
    expect(savingButton.onPressed, isNull);
    expect(cancelButton.onPressed, isNull);

    profileCompleter.complete(ServiceResult<void>.failure(message: '更新资料失败'));
    await tester.pump();
    await tester.pump();

    expect(toastMessages, hasLength(1));
    expect(toastMessages.single.message, '更新资料失败');
    expect(find.byType(AlertDialog), findsOneWidget);

    final saveButton = tester.widget<ElevatedButton>(
      find.widgetWithText(ElevatedButton, '保存'),
    );
    final retryCancelButton = tester.widget<TextButton>(
      find.byType(TextButton),
    );
    expect(saveButton.onPressed, isNotNull);
    expect(retryCancelButton.onPressed, isNotNull);
  });

  testWidgets('saves introduction together with profile update', (
    tester,
  ) async {
    authService.enqueueProfileResponse(
      Future<ServiceResult<void>>.value(ServiceResult<void>.success()),
    );

    await pumpShell(tester);
    await openDialog(tester);

    await tester.enterText(find.byType(TextField).at(0), '新昵称');
    await tester.enterText(find.byType(TextField).at(1), '新的个人介绍');
    await tester.tap(find.widgetWithText(ElevatedButton, '保存'));
    await tester.pumpAndSettle();

    expect(find.byType(AlertDialog), findsNothing);
    expect(authService.user?.nickname, '新昵称');
    expect(authService.user?.introduction, '新的个人介绍');
    expect(toastMessages, hasLength(1));
    expect(toastMessages.single.message, '保存成功');
    expect(toastMessages.single.isError, isFalse);
  });
}
