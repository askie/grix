import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';
import 'package:markdown_widget/markdown_widget.dart';

import '../utils/toast_util.dart';
import 'chat_markdown_style_sheet.dart';

class ChatMarkdownCodeBlockView extends StatelessWidget {
  const ChatMarkdownCodeBlockView({
    super.key,
    required this.code,
    required this.styleSheet,
    this.language,
  });

  final String code;
  final String? language;
  final ChatMarkdownStyleSheet styleSheet;

  static List<InlineSpan> _highlightCode({
    required String code,
    required String? language,
    required ChatMarkdownStyleSheet styleSheet,
  }) {
    final lines = code.split('\n');
    final spans = <InlineSpan>[];

    for (var index = 0; index < lines.length; index++) {
      spans.addAll(
        _highlightLine(
          line: lines[index],
          language: language,
          styleSheet: styleSheet,
        ),
      );
      if (index < lines.length - 1) {
        spans.add(TextSpan(text: '\n', style: styleSheet.preTextStyle));
      }
    }

    return spans;
  }

  static List<InlineSpan> _highlightLine({
    required String line,
    required String? language,
    required ChatMarkdownStyleSheet styleSheet,
  }) {
    try {
      return highLightSpans(
        line,
        language: language,
        theme: styleSheet.preSyntaxTheme,
        textStyle: styleSheet.preTextStyle,
        styleNotMatched: styleSheet.preStyleNotMatched,
      );
    } catch (_) {
      return [TextSpan(text: line, style: styleSheet.preTextStyle)];
    }
  }

  Future<void> _copyCode() async {
    if (code.isEmpty) {
      return;
    }
    await Clipboard.setData(ClipboardData(text: code));
    CustomToast.show('chat_copy_success'.tr, isError: false);
  }

  @override
  Widget build(BuildContext context) {
    final resolvedLanguage = (language == null || language!.isEmpty)
        ? null
        : language;
    final headerColor = styleSheet.preLabelStyle.color;
    final showHeader = resolvedLanguage != null || code.isNotEmpty;

    return Container(
      decoration: styleSheet.preDecoration,
      margin: styleSheet.preMargin,
      padding: const EdgeInsets.fromLTRB(12, 8, 12, 6),
      width: double.infinity,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (showHeader)
            Row(
              children: [
                if (resolvedLanguage != null)
                  Expanded(
                    child: Text(
                      resolvedLanguage,
                      style: styleSheet.preLabelStyle,
                    ),
                  )
                else
                  const Spacer(),
                if (code.isNotEmpty)
                  IconButton(
                    onPressed: () => _copyCode(),
                    tooltip: 'chat_copy'.tr,
                    icon: Icon(
                      Icons.copy_rounded,
                      size: 18,
                      color: headerColor,
                    ),
                    visualDensity: VisualDensity.compact,
                    padding: EdgeInsets.zero,
                    constraints: const BoxConstraints.tightFor(
                      width: 28,
                      height: 28,
                    ),
                    splashRadius: 18,
                  ),
              ],
            ),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Text.rich(
              TextSpan(
                style: styleSheet.preTextStyle,
                children: ChatMarkdownCodeBlockView._highlightCode(
                  code: code,
                  language: resolvedLanguage,
                  styleSheet: styleSheet,
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
