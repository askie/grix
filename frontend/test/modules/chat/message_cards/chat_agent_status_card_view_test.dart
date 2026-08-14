import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/widgets/chat_agent_status_card_view.dart';

Widget _wrap(ChatAgentStatusCardData card) {
  return GetMaterialApp(
    translations: AppTranslations(),
    locale: const Locale('zh', 'CN'),
    home: Scaffold(
      body: ChatAgentStatusCardView(card: card, isMine: false, fontScale: 1),
    ),
  );
}

void main() {
  testWidgets('session success card hides reference id but keeps detail', (
    WidgetTester tester,
  ) async {
    // 绑定成功卡瘦身由后端负责（不再下发 detail_text）；
    // where/status 查询卡仍下发工作区详情，前端有则照显。
    await tester.pumpWidget(
      _wrap(
        const ChatAgentStatusCardData(
          category: 'session',
          status: 'success',
          summary: '已绑定 /tmp/demo',
          detailText: 'Workspace: /tmp/demo\nWorker: ready',
          referenceId: 'sess-uuid-1',
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('已绑定 /tmp/demo'), findsOneWidget);
    expect(find.textContaining('sess-uuid-1'), findsNothing);
    // 后端下发的英文详情模板（Workspace:/Worker:）按客户端语言本地化。
    expect(find.textContaining('运行状态'), findsOneWidget);
  });

  testWidgets('session success card without detail renders summary only', (
    WidgetTester tester,
  ) async {
    // 新版绑定成功卡：后端只下发一句 summary，卡片不出现任何技术详情。
    await tester.pumpWidget(
      _wrap(
        const ChatAgentStatusCardData(
          category: 'session',
          status: 'success',
          summary: '已绑定 /tmp/demo',
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('已绑定 /tmp/demo'), findsOneWidget);
    expect(find.textContaining('Workspace'), findsNothing);
    expect(find.textContaining('Worker'), findsNothing);
  });

  testWidgets('session error card keeps error detail but hides reference id', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      _wrap(
        const ChatAgentStatusCardData(
          category: 'session',
          status: 'error',
          summary: '会话打开失败。',
          detailText: '工作目录不存在，请重新选择。',
          referenceId: 'sess-uuid-2',
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('工作目录不存在，请重新选择。'), findsOneWidget);
    expect(find.textContaining('sess-uuid-2'), findsNothing);
  });

  testWidgets('non-session card still shows reference id and detail', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      _wrap(
        const ChatAgentStatusCardData(
          category: 'question',
          status: 'success',
          summary: '已回复。',
          detailText: '回复内容已转发。',
          referenceId: 'req-9',
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.textContaining('req-9'), findsOneWidget);
    expect(find.text('回复内容已转发。'), findsOneWidget);
  });
}
