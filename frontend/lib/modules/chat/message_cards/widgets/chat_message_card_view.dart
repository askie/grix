import 'package:flutter/material.dart';

import '../models/chat_agent_pairing_card_data.dart';
import '../models/chat_agent_open_session_card_data.dart';
import '../models/chat_agent_question_card_data.dart';
import '../models/chat_agent_status_card_data.dart';
import '../models/chat_call_owner_card_data.dart';
import '../models/chat_exec_approval_card_data.dart';
import '../models/chat_egg_install_status_card_data.dart';
import '../models/chat_exec_status_card_data.dart';
import '../models/chat_conversation_card_data.dart';
import '../models/chat_message_card_action.dart';
import '../models/chat_message_card_data.dart';
import '../models/chat_message_card_type.dart';
import '../models/chat_tool_execution_card_data.dart';
import '../models/chat_tool_execution_group_card_data.dart';
import '../models/chat_user_profile_card_data.dart';
import '../models/chat_thinking_card_data.dart';
import '../models/chat_progress_card_data.dart';
import 'chat_agent_pairing_card_view.dart';
import 'chat_agent_open_session_card_view.dart';
import 'chat_agent_question_card_view.dart';
import 'chat_agent_status_card_view.dart';
import 'chat_call_owner_card_view.dart';
import 'chat_exec_approval_card_view.dart';
import 'chat_egg_install_status_card_view.dart';
import 'chat_exec_status_card_view.dart';
import 'chat_conversation_card_view.dart';
import 'chat_user_profile_card_view.dart';
import 'chat_tool_execution_card_view.dart';
import 'chat_tool_execution_group_card_view.dart';
import 'chat_thinking_card_view.dart';
import 'chat_progress_card_view.dart';
import '../../services/chat_managed_input.dart';

class ChatMessageCardView extends StatelessWidget {
  const ChatMessageCardView({
    super.key,
    required this.card,
    this.sourceMessageId = '',
    required this.isMine,
    required this.fontScale,
    this.onTap,
    this.onAction,
    this.managedInputBinding,
    this.isExecApprovalPending,
    this.pickRemoteDirectory,
  });

  final ChatMessageCardData card;
  final String sourceMessageId;
  final bool isMine;
  final double fontScale;
  final VoidCallback? onTap;
  final ChatMessageCardActionHandler? onAction;
  final ChatManagedInputBinding? managedInputBinding;
  final bool Function(String approvalId)? isExecApprovalPending;
  final Future<String?> Function()? pickRemoteDirectory;

  @override
  Widget build(BuildContext context) {
    switch (card.type) {
      case ChatMessageCardType.userProfile:
        return ChatUserProfileCardView(
          card: card as ChatUserProfileCardData,
          isMine: isMine,
          fontScale: fontScale,
          onTap: onTap,
        );
      case ChatMessageCardType.conversation:
        return ChatConversationCardView(
          card: card as ChatConversationCardData,
          isMine: isMine,
          fontScale: fontScale,
          onTap: onTap,
        );
      case ChatMessageCardType.execApproval:
        final approvalCard = card as ChatExecApprovalCardData;
        return ChatExecApprovalCardView(
          card: approvalCard,
          isMine: isMine,
          fontScale: fontScale,
          isPending:
              isExecApprovalPending?.call(approvalCard.approvalId) ?? false,
          onDecisionTap: onAction == null
              ? null
              : (decision) async => onAction!(
                  ChatMessageCardAction(
                    card: card,
                    actionId: decision,
                    sourceMessageId: sourceMessageId,
                  ),
                ),
        );
      case ChatMessageCardType.execStatus:
        return ChatExecStatusCardView(
          card: card as ChatExecStatusCardData,
          isMine: isMine,
          fontScale: fontScale,
        );
      case ChatMessageCardType.toolExecution:
        return ChatToolExecutionCardView(
          card: card as ChatToolExecutionCardData,
          isMine: isMine,
          fontScale: fontScale,
        );
      case ChatMessageCardType.toolExecutionGroup:
        return ChatToolExecutionGroupCardView(
          card: card as ChatToolExecutionGroupCardData,
          isMine: isMine,
          fontScale: fontScale,
        );
      case ChatMessageCardType.eggInstallStatus:
        return ChatEggInstallStatusCardView(
          card: card as ChatEggInstallStatusCardData,
          isMine: isMine,
          fontScale: fontScale,
        );
      case ChatMessageCardType.agentStatus:
        return ChatAgentStatusCardView(
          card: card as ChatAgentStatusCardData,
          isMine: isMine,
          fontScale: fontScale,
        );
      case ChatMessageCardType.agentQuestion:
        return ChatAgentQuestionCardView(
          card: card as ChatAgentQuestionCardData,
          isMine: isMine,
          fontScale: fontScale,
          managedInputBinding: managedInputBinding,
          onQuickAnswerTap: onAction == null
              ? null
              : (answer) async => onAction!(
                  ChatMessageCardAction(
                    card: card,
                    actionId: answer,
                    sourceMessageId: sourceMessageId,
                  ),
                ),
        );
      case ChatMessageCardType.agentPairing:
        return ChatAgentPairingCardView(
          card: card as ChatAgentPairingCardData,
          isMine: isMine,
          fontScale: fontScale,
        );
      case ChatMessageCardType.agentOpenSession:
        return ChatAgentOpenSessionCardView(
          card: card as ChatAgentOpenSessionCardData,
          isMine: isMine,
          fontScale: fontScale,
          managedInputBinding: managedInputBinding,
          pickRemoteDirectory: pickRemoteDirectory,
          onSubmit: onAction == null
              ? null
              : (command) async => onAction!(
                  ChatMessageCardAction(
                    card: card,
                    actionId: command,
                    sourceMessageId: sourceMessageId,
                  ),
                ),
        );
      case ChatMessageCardType.callOwner:
        return ChatCallOwnerCardView(
          card: card as ChatCallOwnerCardData,
          isMine: isMine,
          fontScale: fontScale,
          onAccept: onAction == null
              ? null
              : () => onAction!(
                  ChatMessageCardAction(
                    card: card,
                    actionId: 'accept',
                    sourceMessageId: sourceMessageId,
                  ),
                ),
        );
      case ChatMessageCardType.thinking:
        return ChatThinkingCardView(
          card: card as ChatThinkingCardData,
          isMine: isMine,
          fontScale: fontScale,
        );
      case ChatMessageCardType.progress:
        return ChatProgressCardView(
          card: card as ChatProgressCardData,
          isMine: isMine,
          fontScale: fontScale,
        );
    }
  }
}
