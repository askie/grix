import 'package:flutter_test/flutter_test.dart';

import 'package:grix/modules/chat/message_cards/models/chat_agent_open_session_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_pairing_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_question_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_conversation_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_approval_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_egg_install_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_exec_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_tool_execution_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_user_profile_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_thinking_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_card_action_encoder.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';

/// Extracts the grix:// URI from a Markdown link in [content].
/// Returns null if no match is found.
String? _extractGrixUri(String content) {
  final match = RegExp(r'\((grix://card/[^)]+)\)').firstMatch(content);
  return match?.group(1);
}

void main() {
  test('user profile card encodes and decodes via grix:// roundtrip', () {
    final envelope = ChatMessageCardCodec.buildUserProfileCard(
      userId: 'agent-9',
      peerType: 2,
      nickname: 'Ops Agent',
      avatarUrl: 'https://example.com/avatar/agent-9.png',
    );

    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
    expect(decoded, isA<ChatUserProfileCardData>());

    final card = decoded as ChatUserProfileCardData;
    expect(card.userId, 'agent-9');
    expect(card.peerType, 2);
    expect(card.isAgent, isTrue);
    expect(card.nickname, 'Ops Agent');
  });

  test('conversation card encodes and decodes via grix:// roundtrip', () {
    final envelope = ChatMessageCardCodec.buildConversationCard(
      sessionId: 'session-1',
      sessionType: 'group',
      title: '测试群',
    );

    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
    expect(decoded, isA<ChatConversationCardData>());

    final card = decoded as ChatConversationCardData;
    expect(card.sessionId, 'session-1');
    expect(card.sessionType, 'group');
    expect(card.title, '测试群');
  });

  test('conversation card builder rejects invalid session type', () {
    expect(
      () => ChatMessageCardCodec.buildConversationCard(
        sessionId: 'session-1',
        sessionType: 'unknown',
        title: '测试群',
      ),
      throwsA(isA<ArgumentError>()),
    );
  });

  test('conversation card decodes from standalone grix markdown message', () {
    final envelope = ChatMessageCardCodec.buildConversationCard(
      sessionId: 'session-9',
      sessionType: 'private',
      title: 'Alice',
      peerId: '1001',
    );

    final decoded = ChatMessageCardCodec.decodeFromMessage(
      content: envelope.content,
    );

    expect(decoded, isA<ChatConversationCardData>());
    final card = decoded as ChatConversationCardData;
    expect(card.sessionId, 'session-9');
    expect(card.sessionType, 'private');
    expect(card.title, 'Alice');
    expect(card.peerId, '1001');
  });

  test('old conversation directive is no longer decoded', () {
    final decoded = ChatMessageCardCodec.decodeFromMessage(
      content:
          '[[conversation-card|session_id=session-10|session_type=group|title=%E4%BA%A7%E5%93%81%E8%AE%A8%E8%AE%BA%E7%BE%A4%20A]]',
    );

    expect(decoded, isNull);
  });

  test('exec approval card encodes and decodes via grix:// roundtrip', () {
    final envelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_123',
      approvalSlug: 'req_123',
      approvalCommandId: 'approval_full_123',
      command: 'rm -rf /tmp/demo && echo done',
      host: 'gateway',
      nodeId: 'node-1',
      cwd: '/tmp/demo',
      expiresInSeconds: 45,
      expiresAtMs: 1711234567890,
      allowedDecisions: const ['allow-once', 'allow-always', 'deny'],
    );

    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
    expect(decoded, isA<ChatExecApprovalCardData>());

    final card = decoded as ChatExecApprovalCardData;
    expect(card.approvalId, 'approval_full_123');
    expect(card.approvalSlug, 'req_123');
    expect(card.approvalCommandId, 'approval_full_123');
    expect(card.command, 'rm -rf /tmp/demo && echo done');
    expect(card.host, 'gateway');
    expect(card.nodeId, 'node-1');
    expect(card.cwd, '/tmp/demo');
    expect(card.expiresInSeconds, 45);
    expect(card.expiresAtMs, 1711234567890);
    expect(card.allowedDecisions, ['allow-once', 'allow-always', 'deny']);
  });

  test('exec approval card decodes from standalone grix markdown message', () {
    final envelope = ChatMessageCardCodec.buildExecApprovalCard(
      approvalId: 'approval_full_456',
      approvalSlug: 'req_456',
      approvalCommandId: 'approval_full_456',
      command: 'echo "Hello, World!"',
      host: 'gateway',
      allowedDecisions: const ['allow-once', 'deny'],
    );

    final decoded = ChatMessageCardCodec.decodeFromMessage(
      content: envelope.content,
    );

    expect(decoded, isA<ChatExecApprovalCardData>());
    final card = decoded as ChatExecApprovalCardData;
    expect(card.approvalId, 'approval_full_456');
    expect(card.approvalSlug, 'req_456');
    expect(card.command, 'echo "Hello, World!"');
    expect(card.host, 'gateway');
    expect(card.allowedDecisions, ['allow-once', 'deny']);
  });

  test('exec status card encodes and decodes via grix:// roundtrip', () {
    final envelope = ChatMessageCardCodec.buildExecStatusCard(
      status: 'resolved-allow-always',
      summary: 'Allow always selected by u_1.',
      detailText: 'Reason: trusted build',
      approvalId: 'approval_full_123',
      approvalCommandId: 'req_123',
      decision: 'allow-always',
      resolvedById: 'u_1',
    );

    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
    expect(decoded, isA<ChatExecStatusCardData>());

    final card = decoded as ChatExecStatusCardData;
    expect(card.status, 'resolved-allow-always');
    expect(card.summary, 'Allow always selected by u_1.');
    expect(card.detailText, 'Reason: trusted build');
    expect(card.approvalId, 'approval_full_123');
    expect(card.approvalCommandId, 'req_123');
    expect(card.decision, 'allow-always');
    expect(card.resolvedById, 'u_1');
  });

  test(
    'exec expired status card encodes and decodes via grix:// roundtrip',
    () {
      final envelope = ChatMessageCardCodec.buildExecStatusCard(
        status: 'approval-expired',
        summary: 'Exec approval expired.',
        detailText: 'unknown or expired approval id',
        approvalId: 'approval_full_999',
        approvalCommandId: 'req_999',
        warningText: 'This approval request is no longer valid.',
      );

      final uri = _extractGrixUri(envelope.content);
      expect(uri, isNotNull);
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
      expect(decoded, isA<ChatExecStatusCardData>());

      final card = decoded as ChatExecStatusCardData;
      expect(card.status, 'approval-expired');
      expect(card.summary, 'Exec approval expired.');
      expect(card.detailText, 'unknown or expired approval id');
      expect(card.warningText, 'This approval request is no longer valid.');
    },
  );

  test('tool execution card encodes and decodes via grix:// roundtrip', () {
    final envelope = ChatMessageCardCodec.encode(
      const ChatToolExecutionCardData(
        summaryText: 'Tool: read /tmp/demo',
        detailText: '```txt\nhello\n```',
      ),
    );

    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
    expect(decoded, isA<ChatToolExecutionCardData>());

    final card = decoded as ChatToolExecutionCardData;
    expect(card.summaryText, 'Tool: read /tmp/demo');
    expect(card.detailText, '```txt\nhello\n```');
  });

  test('egg install status card encodes and decodes via grix:// roundtrip', () {
    final envelope = ChatMessageCardCodec.encode(
      const ChatEggInstallStatusCardData(
        installId: 'eggins_1',
        status: 'running',
        summary: 'Package downloaded',
        step: 'downloaded',
      ),
    );

    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
    expect(decoded, isA<ChatEggInstallStatusCardData>());

    final card = decoded as ChatEggInstallStatusCardData;
    expect(card.installId, 'eggins_1');
    expect(card.status, 'running');
    expect(card.summary, 'Package downloaded');
    expect(card.step, 'downloaded');
  });

  test('detects exec approval resolution directive as internal message', () {
    expect(
      ChatMessageCardCodec.isInternalDirectiveMessage(
        '[[exec-approval-resolution|approval_id=approval_full_123|approval_command_id=req_123|decision=allow-once]]',
      ),
      isTrue,
    );
    expect(
      ChatMessageCardCodec.isInternalDirectiveMessage(
        '[研发群](grix://card/conversation?session_id=session-1&session_type=group&title=%E7%A0%94%E5%8F%91%E7%BE%A4)',
      ),
      isFalse,
    );
  });

  test('detects standalone card action directives as internal messages', () {
    expect(
      ChatMessageCardCodec.isInternalDirectiveMessage(
        'grix://open/session?cwd=%2Fworkspace%2Fproject',
      ),
      isTrue,
    );
    expect(
      ChatMessageCardCodec.isInternalDirectiveMessage(
        'grix://card/agent_question_reply?d=%7B%22request_id%22%3A%22question-2%22%2C%22action%22%3A%22accept%22%7D',
      ),
      isTrue,
    );
    expect(
      ChatMessageCardCodec.isInternalDirectiveMessage(
        'grix://card/conversation?session_id=session-1&session_type=group&title=%E7%A0%94%E5%8F%91%E7%BE%A4',
      ),
      isFalse,
    );
  });

  test('builds structured agent question command for multiple questions', () {
    const card = ChatAgentQuestionCardData(
      requestId: 'question-2',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose environment.',
          options: ['prod', 'staging'],
        ),
        ChatAgentQuestionPrompt(
          index: 2,
          header: 'Region',
          prompt: 'Choose region.',
        ),
      ],
    );

    expect(
      ChatAgentCardActionEncoder.buildQuestionStructuredReplyAction(card, {
        1: 'prod',
        2: 'cn-hz',
      }),
      'grix://card/agent_question_reply?d=%7B%22request_id%22%3A%22question-2%22%2C%22response%22%3A%7B%22type%22%3A%22map%22%2C%22entries%22%3A%5B%7B%22key%22%3A%221%22%2C%22value%22%3A%22prod%22%7D%2C%7B%22key%22%3A%222%22%2C%22value%22%3A%22cn-hz%22%7D%5D%7D%7D',
    );
  });

  test('agent open session card encodes and decodes via grix:// roundtrip', () {
    final envelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'open 缺少目录路径。',
      detailText: '请输入工作目录来启动或恢复当前会话。',
      initialCwd: '/tmp/demo',
      submittedPath: '/workspace/project',
    );

    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
    expect(decoded, isA<ChatAgentOpenSessionCardData>());

    final card = decoded as ChatAgentOpenSessionCardData;
    expect(card.displaySummaryText, 'open 缺少目录路径。');
    expect(card.displayDetailText, '请输入工作目录来启动或恢复当前会话。');
    expect(card.displayInitialCwd, '/tmp/demo');
    expect(card.displaySubmittedPath, '/workspace/project');
    expect(
      ChatAgentCardActionEncoder.buildOpenSessionAction(
        card,
        '/workspace/project',
      ),
      'grix://open/session?cwd=%2Fworkspace%2Fproject',
    );
  });

  test('agent question card keeps submitted answer when encoded/decoded', () {
    const card = ChatAgentQuestionCardData(
      requestId: 'question-1',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose environment.',
          options: ['prod', 'staging'],
        ),
      ],
      submittedAnswer: 'staging',
    );
    final envelope = ChatMessageCardCodec.encode(card);

    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    final decoded = ChatMessageCardCodec.decodeGrixUriCard(uri!);
    expect(decoded, isA<ChatAgentQuestionCardData>());
    final decodedCard = decoded as ChatAgentQuestionCardData;
    expect(decodedCard.displaySubmittedAnswer, 'staging');
  });

  test('thinking card decodes from standalone grix markdown message', () {
    final decoded = ChatMessageCardCodec.decodeFromMessage(
      content:
          '[Thinking](grix://card/thinking?content=first+line%0Asecond+line)',
    );

    expect(decoded, isA<ChatThinkingCardData>());
    final card = decoded as ChatThinkingCardData;
    expect(card.displayContent, 'first line\nsecond line');
  });

  test('encode produces agent_status type for agent status card', () {
    const statusCard = ChatAgentStatusCardData(
      category: 'approval',
      status: 'info',
      summary: 'Processing.',
    );
    final envelope = ChatMessageCardCodec.encode(statusCard);
    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    expect(uri, contains('agent_status'));
  });

  test('encode produces agent_question type for question card', () {
    const questionCard = ChatAgentQuestionCardData(
      requestId: 'q-1',
      questions: [
        ChatAgentQuestionPrompt(index: 1, header: 'Env', prompt: 'Choose.'),
      ],
    );
    final envelope = ChatMessageCardCodec.encode(questionCard);
    expect(envelope.extra, isEmpty);
    final uri = _extractGrixUri(envelope.content);
    expect(uri, isNotNull);
    expect(uri, contains('agent_question'));
  });

  test('reply_source text fallback is no longer decoded as a card', () {
    final decoded = ChatMessageCardCodec.decodeFromMessage(
      content: '''
Please answer the following question.
Request ID: question-1
Question: Choose the deployment target.
Respond by typing your answer after the request id.
''',
    );

    expect(decoded, isNull);
  });

  group('grix:// URI card decoding', () {
    test('decodes grix:// exec_status URI', () {
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        'grix://card/exec_status?status=running&summary=Processing&host=gateway',
      );
      expect(decoded, isA<ChatExecStatusCardData>());
      final card = decoded as ChatExecStatusCardData;
      expect(card.status, 'running');
      expect(card.summary, 'Processing');
      expect(card.host, 'gateway');
    });

    test('decodes grix:// exec_approval URI with comma-separated array', () {
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        'grix://card/exec_approval?approval_id=req1&approval_slug=req1&approval_command_id=req1&command=pwd&host=gw&allowed_decisions=allow-once,deny',
      );
      expect(decoded, isA<ChatExecApprovalCardData>());
      final card = decoded as ChatExecApprovalCardData;
      expect(card.approvalId, 'req1');
      expect(card.command, 'pwd');
    });

    test('decodes grix:// conversation URI', () {
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        'grix://card/conversation?session_id=s1&session_type=group&title=Test',
      );
      expect(decoded, isA<ChatConversationCardData>());
      final card = decoded as ChatConversationCardData;
      expect(card.sessionId, 's1');
      expect(card.sessionType, 'group');
      expect(card.title, 'Test');
    });

    test(
      'decodes grix:// egg_install_status URI with HTML-escaped separators',
      () {
        final decoded = ChatMessageCardCodec.decodeGrixUriCard(
          'grix://card/egg_install_status?install_id=eggins_2042145742675513344&amp;status=running&amp;step=installed&amp;summary=%E7%8E%B0%E5%9C%A8%E9%85%8D%E7%BD%AE%E6%9C%AC%E5%9C%B0%20OpenClaw%E3%80%82%E5%AE%89%E8%A3%85%E5%86%85%E5%AE%B9%E5%B7%B2%E8%90%BD%E4%BD%8D%EF%BC%8C%E6%A0%A1%E9%AA%8C%E4%B8%AD',
        );
        expect(decoded, isA<ChatEggInstallStatusCardData>());
        final card = decoded as ChatEggInstallStatusCardData;
        expect(card.installId, 'eggins_2042145742675513344');
        expect(card.status, 'running');
        expect(card.step, 'installed');
        expect(card.summary, '现在配置本地 OpenClaw。安装内容已落位，校验中');
      },
    );

    test(
      'decodes grix:// egg_install_status URI with malformed percent summary',
      () {
        final decoded = ChatMessageCardCodec.decodeGrixUriCard(
          'grix://card/egg_install_status?install_id=eggins_2042145742675513344&status=running&step=installe&summary=%E7%8E%B0%E5%9C%A8%E9%85%8D%E7%BD%AE%E6%9C%AC%E5%9C%B0%20OpenClaw%E3%80%82%E5%AE%89%E8%A3%85%E5%86%85%E5%AE%B9%E5%B7%B2%E8%90%BD%E4%BD%8D%EF%BC%8C%E6%A0%A1%E9%AA%8C%E4%B8%AD%',
        );
        expect(decoded, isA<ChatEggInstallStatusCardData>());
        final card = decoded as ChatEggInstallStatusCardData;
        expect(card.installId, 'eggins_2042145742675513344');
        expect(card.status, 'running');
        expect(card.step, 'installe');
        expect(card.summary, startsWith('现在配置本地 OpenClaw。安装内容已落位，校验中'));
      },
    );

    test('decodes grix:// agent_status URI', () {
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        'grix://card/agent_status?category=approval&status=info&summary=Processing',
      );
      expect(decoded, isA<ChatAgentStatusCardData>());
      final card = decoded as ChatAgentStatusCardData;
      expect(card.category, 'approval');
      expect(card.status, 'info');
    });

    test('decodes grix:// complex payload via d parameter', () {
      final payload = Uri.encodeComponent(
        '{"request_id":"q-1","questions":[{"index":1,"header":"Env","prompt":"Choose."}]}',
      );
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        'grix://card/agent_question?d=$payload',
      );
      expect(decoded, isA<ChatAgentQuestionCardData>());
      final card = decoded as ChatAgentQuestionCardData;
      expect(card.requestId, 'q-1');
      expect(card.questions, hasLength(1));
    });

    test('returns null for non-grix URI', () {
      expect(
        ChatMessageCardCodec.decodeGrixUriCard('https://example.com'),
        isNull,
      );
    });

    test('returns null for grix:// non-card URI', () {
      expect(
        ChatMessageCardCodec.decodeGrixUriCard('grix://other/path'),
        isNull,
      );
    });

    test('returns null for invalid d parameter JSON', () {
      expect(
        ChatMessageCardCodec.decodeGrixUriCard(
          'grix://card/agent_question?d=not-json',
        ),
        isNull,
      );
    });

    test('decodes grix:// agent_pairing URI', () {
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        'grix://card/agent_pairing?pairing_code=ABC123&command_hint=%2Fgrix+access+pair+%3Ccode%3E',
      );
      expect(decoded, isA<ChatAgentPairingCardData>());
      final card = decoded as ChatAgentPairingCardData;
      expect(card.pairingCode, 'ABC123');
      expect(card.displayCommandHint, '/grix access pair <code>');
    });

    test('decodes grix:// agent_open_session URI', () {
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        'grix://card/agent_open_session?summary_text=Open+workspace&detail_text=Provide+cwd',
      );
      expect(decoded, isA<ChatAgentOpenSessionCardData>());
      final card = decoded as ChatAgentOpenSessionCardData;
      expect(card.displaySummaryText, 'Open workspace');
      expect(card.displayDetailText, 'Provide cwd');
    });

    test('decodes grix:// thinking URI', () {
      final decoded = ChatMessageCardCodec.decodeGrixUriCard(
        'grix://card/thinking?content=alpha%0Abeta',
      );
      expect(decoded, isA<ChatThinkingCardData>());
      final card = decoded as ChatThinkingCardData;
      expect(card.displayContent, 'alpha\nbeta');
    });
  });
}
