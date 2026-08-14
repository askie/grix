import 'package:flutter/material.dart';
import 'package:markdown_widget/markdown_widget.dart';

import '../../modules/chat/message_cards/models/chat_message_card_action.dart';
import '../../modules/chat/message_cards/models/chat_message_card_data.dart';
import '../../modules/chat/services/chat_managed_input.dart';
import '../markdown/chat_markdown_dialect.dart';
import '../markdown/chat_markdown_uri_policy.dart';
import '../utils/app_external_links.dart';
import 'chat_markdown_style_sheet.dart';
import 'chat_selection_area.dart';
import 'latex_support.dart';
import 'markdown_builder.dart';

class ChatMarkdownFallbackView extends StatelessWidget {
  const ChatMarkdownFallbackView({
    super.key,
    required this.data,
    required this.styleSheet,
    this.onMessageCardAction,
    this.onMessageCardTap,
    this.sourceMessageId = '',
    this.managedInputBinding,
    this.isExecApprovalPending,
    this.pickRemoteDirectory,
    this.selectionEnabled = true,
    this.onSelectionCleared,
  });

  final String data;
  final ChatMarkdownStyleSheet styleSheet;
  final ChatMessageCardActionHandler? onMessageCardAction;
  final ValueChanged<ChatMessageCardData>? onMessageCardTap;
  final String sourceMessageId;
  final ChatManagedInputBinding? managedInputBinding;
  final bool Function(String approvalId)? isExecApprovalPending;
  final Future<String?> Function()? pickRemoteDirectory;
  final bool selectionEnabled;
  final VoidCallback? onSelectionCleared;

  @override
  Widget build(BuildContext context) {
    return ChatSelectionArea(
      enabled: selectionEnabled,
      onSelectionCleared: onSelectionCleared,
      child: MarkdownWidget(
        data: data,
        selectable: false,
        shrinkWrap: true,
        config: MarkdownConfig(
          configs: [
            PConfig(textStyle: styleSheet.paragraphStyle),
            LinkConfig(
              style: styleSheet.linkStyle,
              onTap: (url) {
                final uri = ChatMarkdownUriPolicy.resolveSafeLinkUri(url);
                if (uri != null) {
                  // 收口到统一外链入口，http/https 先过链接黑名单校验。
                  AppExternalLinks.open(uri.toString());
                }
              },
            ),
            CodeConfig(style: styleSheet.inlineCodeStyle),
            PreConfig(
              textStyle: styleSheet.preTextStyle,
              styleNotMatched: styleSheet.preStyleNotMatched,
              theme: styleSheet.preSyntaxTheme,
              decoration: styleSheet.preDecoration,
              margin: styleSheet.preMargin,
              padding: styleSheet.prePadding,
            ),
            TableConfig(
              headerStyle: styleSheet.tableHeaderStyle,
              bodyStyle: styleSheet.tableBodyStyle,
              border: TableBorder.all(
                color: styleSheet.tableBorderColor,
                width: 1,
              ),
            ),
          ],
        ),
        markdownGenerator: MarkdownGenerator(
          generators: [
            customImageGenerator(),
            customLinkGenerator(
              onMessageCardAction: onMessageCardAction,
              onMessageCardTap: onMessageCardTap,
              sourceMessageId: sourceMessageId,
              managedInputBinding: managedInputBinding,
              isExecApprovalPending: isExecApprovalPending,
              pickRemoteDirectory: pickRemoteDirectory,
            ),
            SpanNodeGeneratorWithTag(
              tag: MarkdownTag.pre.name,
              generator: (element, config, _) =>
                  CustomPreNode(element, config.pre, styleSheet),
            ),
            latexBlockGenerator(textColor: styleSheet.blockMathStyle.color!),
            latexInlineGenerator(textColor: styleSheet.inlineMathStyle.color!),
          ],
          extensionSet: ChatMarkdownDialect.buildExtensionSet(),
        ),
      ),
    );
  }
}
