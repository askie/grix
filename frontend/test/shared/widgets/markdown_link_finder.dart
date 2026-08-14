import 'package:flutter/gestures.dart';
import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';

/// 链接从独立的 `ChatMarkdownLinkView` 组件重构为
/// `TextSpan` + `TapGestureRecognizer`（见 chat_markdown_inline_renderer.dart）后，
/// 无法再用 `find.byType(ChatMarkdownLinkView)` 断言链接是否渲染。
///
/// 这里遍历已渲染的 [RichText]（native ast 路径用 `Text.rich` 承载内联内容），
/// 收集挂了点击手势的内联 span——即“可点链接”。非法 scheme 会被降级成不带
/// recognizer 的纯文本 span，因此不会被收进来。
List<TextSpan> tappableLinkSpans(WidgetTester tester) {
  final spans = <TextSpan>[];
  for (final richText in tester.widgetList<RichText>(find.byType(RichText))) {
    richText.text.visitChildren((span) {
      if (span is TextSpan && span.recognizer is TapGestureRecognizer) {
        spans.add(span);
      }
      return true;
    });
  }
  return spans;
}

/// 可点链接的显示文案（链接 label，裸 URL 链接时是 URL 本身）。
List<String> tappableLinkTexts(WidgetTester tester) =>
    tappableLinkSpans(tester).map((span) => span.text ?? '').toList();
