import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_card_text_localizer.dart';

/// 后端把绑定卡/会话控制卡文案以固定模板（中英混杂）拼好下发，
/// 本地化器负责按客户端语言重新渲染；这里验证 zh/en 双向映射与兜底。
Future<void> _pumpLocale(WidgetTester tester, Locale locale) async {
  await tester.pumpWidget(
    GetMaterialApp(
      translations: AppTranslations(),
      locale: locale,
      home: const SizedBox.shrink(),
    ),
  );
}

void main() {
  group('zh_CN locale', () {
    testWidgets('translates backend English templates to Chinese', (
      WidgetTester tester,
    ) async {
      await _pumpLocale(tester, const Locale('zh', 'CN'));

      expect(
        ChatAgentCardTextLocalizer.localize('已绑定 /tmp/demo'),
        '已绑定 /tmp/demo',
      );
      expect(
        ChatAgentCardTextLocalizer.localize(
          'Claude session stopped for /a/b.',
        ),
        'Claude 会话已停止（/a/b）。',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('Codex session restarted.'),
        'Codex 会话已重启。',
      );
      expect(
        ChatAgentCardTextLocalizer.localize(
          'Current Claude workspace is /tmp/demo.',
        ),
        '当前 Claude 工作目录：/tmp/demo',
      );
      expect(
        ChatAgentCardTextLocalizer.localize(
          'No Claude session is bound to this chat.',
        ),
        '当前会话未绑定 Claude。',
      );
      expect(
        ChatAgentCardTextLocalizer.localize(
          'Claude session could not be opened.',
        ),
        'Claude 会话打开失败。',
      );
    });

    testWidgets('exact sentences win over provider templates', (
      WidgetTester tester,
    ) async {
      await _pumpLocale(tester, const Locale('zh', 'CN'));

      // 若先套模板会把句首的 "A" 误当 provider 名，这里锁死精确匹配优先。
      expect(
        ChatAgentCardTextLocalizer.localize('A workspace path is required.'),
        '需要提供工作目录路径。',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('The workspace path is invalid.'),
        '工作目录路径无效。',
      );
    });

    testWidgets('translates multi-line detail text line by line', (
      WidgetTester tester,
    ) async {
      await _pumpLocale(tester, const Locale('zh', 'CN'));

      expect(
        ChatAgentCardTextLocalizer.localize(
          'Workspace: /tmp/demo\nWorker: ready',
        ),
        '工作目录：/tmp/demo\n运行状态：ready',
      );
    });

    testWidgets('keeps unknown text untouched', (WidgetTester tester) async {
      await _pumpLocale(tester, const Locale('zh', 'CN'));

      expect(
        ChatAgentCardTextLocalizer.localize('自定义插件返回的其他文案'),
        '自定义插件返回的其他文案',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('Some unexpected new sentence.'),
        'Some unexpected new sentence.',
      );
      expect(ChatAgentCardTextLocalizer.localize(''), '');
    });

    testWidgets('translates exec approval exact sentences', (
      WidgetTester tester,
    ) async {
      await _pumpLocale(tester, const Locale('zh', 'CN'));

      expect(
        ChatAgentCardTextLocalizer.localize('Exec approval allowed once.'),
        '已允许执行一次。',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('Exec approval denied.'),
        '已拒绝执行。',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('Reply recorded.'),
        '回复已记录。',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('Paired! Say hi to Claude.'),
        '配对成功！和 Claude 打个招呼吧。',
      );
    });

    testWidgets('translates question/pairing request templates with params', (
      WidgetTester tester,
    ) async {
      await _pumpLocale(tester, const Locale('zh', 'CN'));

      expect(
        ChatAgentCardTextLocalizer.localize(
          'Question request req-1 answers recorded.',
        ),
        '问答请求 req-1 的回答已记录。',
      );
      expect(
        ChatAgentCardTextLocalizer.localize(
          'Pairing request pair-1 was denied. Ask the Claude Code user to '
          'request a new pairing code if you still need access.',
        ),
        '配对请求 pair-1 已被拒绝。如果仍需要访问，请让 Claude Code 用户重新生成配对码。',
      );
    });

  });

  group('en_US locale', () {
    testWidgets('translates backend Chinese templates to English', (
      WidgetTester tester,
    ) async {
      await _pumpLocale(tester, const Locale('en', 'US'));

      expect(
        ChatAgentCardTextLocalizer.localize('已绑定 /tmp/demo'),
        'Bound to /tmp/demo',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('目录绑定成功。'),
        'Workspace bound successfully.',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('已解绑工作目录。'),
        'Workspace unbound.',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('会话已过期，请新建会话后继续对话。'),
        'This session has expired. Start a new chat to continue.',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('Claude 会话操作超时。'),
        'Claude session action timed out.',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('插件未在规定时间内响应，请稍后重试。'),
        'The plugin did not respond in time. Please try again later.',
      );
    });

    testWidgets('English templates render English', (
      WidgetTester tester,
    ) async {
      await _pumpLocale(tester, const Locale('en', 'US'));

      expect(
        ChatAgentCardTextLocalizer.localize('Claude session stopped.'),
        'Claude session stopped.',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('Workspace: /tmp/demo'),
        'Workspace: /tmp/demo',
      );
    });

    testWidgets('translates thread compact and session usage sentences', (
      WidgetTester tester,
    ) async {
      await _pumpLocale(tester, const Locale('en', 'US'));

      expect(
        ChatAgentCardTextLocalizer.localize('上下文压缩完成。'),
        'Context compaction complete.',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('当前会话尚未绑定，无法查询用量。'),
        'This chat is not bound yet, so usage cannot be queried.',
      );
    });

    testWidgets('Gemini-specific toggle patterns win over generic ones', (
      WidgetTester tester,
    ) async {
      await _pumpLocale(tester, const Locale('en', 'US'));

      // Gemini 专用模式必须排在通用模式之前，否则 "Gemini xx已切换为 yy。"
      // 会被通用 "(.+?)已切换为 (.+?)。" 误把 "Gemini xx" 整体当 type 捕获。
      expect(
        ChatAgentCardTextLocalizer.localize('Gemini 模型已切换为 gemini-pro。'),
        'Gemini Model switched to gemini-pro.',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('切换 Gemini 模型失败。'),
        'Failed to switch Gemini Model.',
      );
      // 通用模式（非 Gemini）照常命中。
      expect(
        ChatAgentCardTextLocalizer.localize('模型已切换为 gpt-5。'),
        'Model switched to gpt-5.',
      );
      expect(
        ChatAgentCardTextLocalizer.localize('切换模型失败。'),
        'Failed to switch Model.',
      );
    });
  });
}
