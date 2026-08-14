import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/report_service.dart';
import 'package:grix/modules/report/models/report_attachment_draft.dart';
import 'package:grix/modules/report/controllers/report_controller.dart';
import 'package:grix/modules/report/report_view.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
  });

  tearDown(Get.reset);

  ReportController pumpController() {
    return Get.put(
      ReportController(
        initialArguments: const <String, dynamic>{
          'target_type': 'group',
          'target_session_id': 'session-1',
          'title': '群聊',
          'subtitle': '2 成员',
        },
        reportService: ReportService(),
        ossService: OssService(),
      ),
    );
  }

  Future<void> pumpReportView(WidgetTester tester) async {
    await tester.pumpWidget(
      GetMaterialApp(
        theme: AppTheme.lightTheme,
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const ReportView(),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('tapping a reason option marks it as selected', (tester) async {
    final controller = pumpController();
    await pumpReportView(tester);

    final violenceOption = find.byKey(const Key('report_reason_violence'));
    expect(violenceOption, findsOneWidget);
    expect(controller.selectedReasonCode.value, isEmpty);

    await tester.tap(violenceOption);
    await tester.pumpAndSettle();

    expect(controller.selectedReasonCode.value, 'violence');

    final animatedContainer = tester.widget<AnimatedContainer>(
      find.byKey(const Key('report_reason_violence_container')),
    );
    final decoration = animatedContainer.decoration! as BoxDecoration;
    final border = decoration.border! as Border;

    expect(border.top.color, AppTheme.primaryColor);
    expect(border.top.width, 2);
  });

  testWidgets('tapping remove deletes uploaded attachment', (tester) async {
    final controller = pumpController();
    controller.attachments.add(
      ReportAttachmentDraft(
        fileName: 'evidence.png',
        contentType: 'image/png',
        bytes: Uint8List.fromList(const <int>[
          0x89,
          0x50,
          0x4E,
          0x47,
          0x0D,
          0x0A,
          0x1A,
          0x0A,
        ]),
      ),
    );

    await pumpReportView(tester);

    final removeButton = find.byKey(const Key('report_attachment_remove_0'));
    expect(removeButton, findsOneWidget);
    expect(controller.attachments, hasLength(1));

    await tester.ensureVisible(removeButton);
    await tester.pumpAndSettle();
    await tester.tap(removeButton);
    await tester.pumpAndSettle();

    expect(controller.attachments, isEmpty);
    expect(find.byKey(const Key('report_attachment_remove_0')), findsNothing);
  });

  testWidgets('submit shows inline validation feedback', (tester) async {
    final controller = pumpController();
    await pumpReportView(tester);

    expect(controller.feedbackMessage.value, isNull);
    await controller.submit();
    await tester.pumpAndSettle();

    expect(controller.feedbackMessage.value, '请选择举报原因');
    expect(controller.feedbackIsError.value, isTrue);
  });
}
