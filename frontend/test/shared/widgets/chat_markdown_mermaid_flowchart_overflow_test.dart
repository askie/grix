import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';
import 'package:grix/shared/widgets/chat_markdown_mermaid_flowchart_view.dart';

// 回归：流程图节点文字被遮挡。
// 根因——布局测量节点框高度用裸 TextPainter（fontFamily=null → 引擎默认字体），
// 而渲染端 Text 的 fontFamily 由 DefaultTextStyle 注入（如 Roboto / iOS 系统字体）。
// 两者字形宽度不同会让渲染换行比测量多，文字溢出固定高度的节点框、末行被切。
// 与系统字体缩放无关。修复：测量改用渲染实际生效的同一套字体。
// 本测试在真实控件树里，按每个节点框的真实尺寸校验渲染文字不溢出。
const _source = '''flowchart TD
    A[start 下发 relay_mode=false] --> B[build_realtime_model<br/>openai RealtimeModel gpt-realtime<br/>turn_detection=server_vad<br/>create_response=False]
    B --> C[AgentSession 启动<br/>挂 OpenAIVoiceOrchestrator]
    C --> D[用户说话 GPT内置STT转写]
    D --> E[发布用户转写到NATS<br/>触发文字大脑]
    E --> I[文字大脑生成答案<br/>NATS on_inject]
    I --> J[on_brain_inject<br/>注入权威资料到GPT上下文]
    J --> H[round+1<br/>启动抢答计时器 800ms]
    H --> L[立即 generate_reply<br/>带权威资料 又快又准]
    L --> M[仅作滚动上下文<br/>不打断已说内容]
    M --> N[计时器超时<br/>凭GPT内置知识先开口 不冷场]
    N --> Q[set_muted True<br/>只听不说 上下文继续积累]''';

void main() {
  testWidgets('节点文字不溢出节点框（含 DefaultTextStyle 注入字体场景）', (
    WidgetTester tester,
  ) async {
    final parsed = const ChatMermaidParser().parse(_source);
    final diagram = parsed.diagram as ChatMermaidFlowchart;

    await tester.pumpWidget(
      MaterialApp(
        // 用真实主题：其 DefaultTextStyle 会给节点 Text 注入 UI 字体，
        // 复现"测量字体≠渲染字体"的线上场景。
        theme: AppTheme.lightTheme,
        home: MediaQuery(
          data: const MediaQueryData(textScaler: TextScaler.noScaling),
          child: Scaffold(
            body: Center(
              child: SizedBox(
                width: 1200,
                height: 1600,
                child: ChatMarkdownMermaidFlowchartView(
                  diagram: diagram,
                  textStyle: const TextStyle(
                    color: Color(0xFF2A2214),
                    fontSize: 13,
                    fontFamily: 'monospace',
                  ),
                  backgroundColor: const Color(0xFFFFFFFF),
                ),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    var checked = 0;
    for (final paragraph in tester.renderObjectList<RenderParagraph>(
      find.byType(RichText),
    )) {
      // 沿父链找到承载该段落的节点框 RenderPadding（其尺寸即布局固定的框尺寸）。
      RenderObject? cursor = paragraph.parent;
      RenderPadding? cardPadding;
      var hops = 0;
      while (cursor != null && hops < 6) {
        if (cursor is RenderPadding) {
          cardPadding = cursor;
          break;
        }
        cursor = cursor.parent;
        hops++;
      }
      if (cardPadding == null) continue; // 边标签药丸结构不同，跳过
      checked++;
      final pad = cardPadding.padding.resolve(TextDirection.ltr);
      final availWidth = cardPadding.size.width - pad.horizontal;
      final availHeight = cardPadding.size.height - pad.vertical;
      // 用渲染实际生效的 text/style/scaler，按节点框可用宽度复排，
      // 真实换行后的高度必须落在框内（不被框底裁切）。
      final painter = TextPainter(
        text: paragraph.text,
        textAlign: paragraph.textAlign,
        textDirection: paragraph.textDirection,
        textScaler: paragraph.textScaler,
      )..layout(maxWidth: availWidth);
      expect(
        painter.height,
        lessThanOrEqualTo(availHeight + 0.5),
        reason:
            '节点「${paragraph.text.toPlainText().replaceAll('\n', ' / ')}」'
            '渲染文字高度 ${painter.height} 超出节点框可用高度 $availHeight，会被遮挡',
      );
    }
    expect(checked, greaterThan(0), reason: '应至少校验到若干节点段落');
  });
}
