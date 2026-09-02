import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix_admin/modules/users/admin_user_item.dart';
import 'package:grix_admin/shared/controllers/user_directory.dart';
import 'package:grix_admin/shared/widgets/user_ref.dart';

void main() {
  setUp(Get.reset);
  tearDown(Get.reset);

  UserDirectory installDirectory(
    Future<List<AdminUserItem>> Function(List<String>) lookupFn,
  ) {
    final dir = UserDirectory()..lookupFn = lookupFn;
    Get.put<UserDirectory>(dir, permanent: true);
    return dir;
  }

  testWidgets('解析完成后 ID 渲染成昵称', (tester) async {
    installDirectory(
      (ids) async => [
        AdminUserItem.fromJson({
          'id': '42',
          'username': 'alice',
          'nickname': '爱丽丝',
          'status': 1,
        }),
      ],
    );

    await tester.pumpWidget(
      const GetMaterialApp(home: Scaffold(body: UserRef('42'))),
    );

    // 解析前先显示裸 ID 兜底。
    expect(find.text('42'), findsOneWidget);

    // 等批量窗口 + 请求完成后，应显示昵称。
    await tester.pump(const Duration(milliseconds: 200));
    expect(find.text('爱丽丝'), findsOneWidget);
    expect(find.text('42'), findsNothing);
  });

  testWidgets('占位名优先于裸 ID，showId 追加 ID', (tester) async {
    installDirectory((ids) async => const []);

    await tester.pumpWidget(
      const GetMaterialApp(
        home: Scaffold(
          body: UserRef('77', placeholderName: '老王', showId: true),
        ),
      ),
    );

    expect(find.text('老王（ID 77）'), findsOneWidget);
    // 负缓存后仍保留占位名。
    await tester.pump(const Duration(milliseconds: 200));
    expect(find.text('老王（ID 77）'), findsOneWidget);
  });

  testWidgets('空 ID 渲染占位横线且不发请求', (tester) async {
    var called = false;
    installDirectory((ids) async {
      called = true;
      return const [];
    });

    await tester.pumpWidget(
      const GetMaterialApp(home: Scaffold(body: UserRef(''))),
    );
    await tester.pump(const Duration(milliseconds: 200));

    expect(find.text('-'), findsOneWidget);
    expect(called, isFalse);
  });
}
