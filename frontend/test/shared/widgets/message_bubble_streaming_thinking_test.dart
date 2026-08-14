import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'package:grix/app/settings/chat_thinking_collapse_service.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/widgets/chat_thinking_card_view.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:grix/shared/widgets/stream_pending_indicator.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() async {
    SharedPreferences.setMockInitialValues({});
    MessageStreamController.resetForTest();
    MessageBubble.resetFinalRenderCacheForTest();
    await Get.putAsync<ChatThinkingCollapseService>(
      () => ChatThinkingCollapseService().init(),
    );
  });

  tearDown(() {
    MessageStreamController.resetForTest();
    MessageBubble.resetFinalRenderCacheForTest();
    Get.reset();
  });

  Widget buildBubble({required String msgId, required bool isThinking}) {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('zh', 'CN'),
      home: Scaffold(
        body: MessageBubble(
          msgId: msgId,
          initialContent: '',
          isStreaming: true,
          isThinking: isThinking,
          isMine: false,
        ),
      ),
    );
  }

  testWidgets('streaming thinking renders thinking card with live buffer', (
    tester,
  ) async {
    const msgId = 'thinking_live';
    await tester.pumpWidget(buildBubble(msgId: msgId, isThinking: true));

    // 空 buffer:仅 pending 指示器,不渲染思考卡片。
    expect(find.byType(StreamPendingIndicator), findsOneWidget);
    expect(find.byType(ChatThinkingCardView), findsNothing);

    MessageStreamController.addChunk(msgId, '正在分析问题');
    await tester.pump(const Duration(milliseconds: 100));

    // buffer 非空:渲染思考卡片并显示实时内容。
    expect(find.byType(StreamPendingIndicator), findsNothing);
    expect(find.byType(ChatThinkingCardView), findsOneWidget);
    expect(find.textContaining('正在分析问题'), findsOneWidget);

    // 追加分片 → 卡片内容实时增长。
    MessageStreamController.addChunk(msgId, ',再核对一遍');
    await tester.pump(const Duration(milliseconds: 100));
    expect(find.textContaining('再核对一遍'), findsOneWidget);
  });

  testWidgets('streaming thinking card floats without white bubble chrome', (
    tester,
  ) async {
    const msgId = 'thinking_float';
    await tester.pumpWidget(buildBubble(msgId: msgId, isThinking: true));

    MessageStreamController.addChunk(msgId, '正在分析问题');
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.byType(ChatThinkingCardView), findsOneWidget);

    // 思考卡片应像工具卡片一样独立悬浮:外层不再有白色气泡容器包裹。
    final bubbleContainer = find.ancestor(
      of: find.byType(ChatThinkingCardView),
      matching: find.byWidgetPredicate((widget) {
        if (widget is! Container) return false;
        final decoration = widget.decoration;
        return decoration is BoxDecoration &&
            decoration.color == const Color(0xFFFFFFFF);
      }),
    );
    expect(bubbleContainer, findsNothing);
  });

  testWidgets('non-thinking streaming keeps plain text path', (tester) async {
    const msgId = 'answer_live';
    await tester.pumpWidget(buildBubble(msgId: msgId, isThinking: false));

    MessageStreamController.addChunk(msgId, '这是正文');
    await tester.pump(const Duration(milliseconds: 100));

    expect(find.byType(ChatThinkingCardView), findsNothing);
    expect(find.textContaining('这是正文'), findsOneWidget);
  });
}
