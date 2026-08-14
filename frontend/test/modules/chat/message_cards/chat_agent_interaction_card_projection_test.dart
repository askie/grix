import 'package:flutter_test/flutter_test.dart';

import 'package:grix/data/models/message_model.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_open_session_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_question_card_data.dart';
import 'package:grix/modules/chat/message_cards/models/chat_agent_status_card_data.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_card_action_encoder.dart';
import 'package:grix/modules/chat/message_cards/services/chat_agent_interaction_card_projection.dart';
import 'package:grix/modules/chat/message_cards/services/chat_message_card_codec.dart';

void main() {
  test(
    'agent interaction projection maps submitted workspace back to card',
    () {
      final openEnvelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
        summaryText: 'open missing cwd',
        detailText: 'send cwd',
      );

      final projection = ChatAgentInteractionCardProjector.project([
        MessageModel(
          msgId: 'm1',
          sessionId: 's1',
          senderId: 'agent-1',
          senderType: 2,
          createdAt: 1000,
          content: openEnvelope.content,
          extra: openEnvelope.extra,
        ),
        MessageModel(
          msgId: 'm2',
          sessionId: 's1',
          senderId: 'u-1',
          senderType: 1,
          createdAt: 1100,
          content: 'grix://open/session?cwd=%2Fworkspace%2Fdemo',
        ),
      ]);

      final overridden = projection.overridesByIndex[0];
      expect(projection.hiddenIndexes, isEmpty);
      expect(overridden, isA<ChatAgentOpenSessionCardData>());
      expect(
        (overridden as ChatAgentOpenSessionCardData).displaySubmittedPath,
        '/workspace/demo',
      );
      expect(overridden.submissionStatus, isNull);
    },
  );

  test('open session projection isolates cards by card instance id', () {
    final openEnvelopeA = ChatMessageCardCodec.buildAgentOpenSessionCard(
      cardInstanceId: 'card-open-a',
      summaryText: 'open missing cwd a',
    );
    final openEnvelopeB = ChatMessageCardCodec.buildAgentOpenSessionCard(
      cardInstanceId: 'card-open-b',
      summaryText: 'open missing cwd b',
    );
    final statusEnvelopeB = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'session',
        status: 'success',
        summary: 'Agent B session opened.',
        referenceId: 'session-1',
        cardInstanceId: 'card-open-b',
      ),
    );

    final projection = ChatAgentInteractionCardProjector.project([
      MessageModel(
        msgId: 'm1',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1000,
        content: openEnvelopeA.content,
        extra: openEnvelopeA.extra,
      ),
      MessageModel(
        msgId: 'm2',
        sessionId: 's1',
        senderId: 'agent-2',
        senderType: 2,
        createdAt: 1100,
        content: openEnvelopeB.content,
        extra: openEnvelopeB.extra,
      ),
      MessageModel(
        msgId: 'm3',
        sessionId: 's1',
        senderId: 'u-1',
        senderType: 1,
        createdAt: 1200,
        content:
            'grix://open/session?cwd=%2Fworkspace%2Fagent-b&card_instance_id=card-open-b',
      ),
      MessageModel(
        msgId: 'm4',
        sessionId: 's1',
        senderId: 'agent-2',
        senderType: 2,
        createdAt: 1300,
        content: statusEnvelopeB.content,
        extra: statusEnvelopeB.extra,
      ),
    ]);

    expect(projection.overridesByIndex[0], isNull);
    final overridden = projection.overridesByIndex[1];
    expect(overridden, isA<ChatAgentOpenSessionCardData>());
    expect(
      (overridden as ChatAgentOpenSessionCardData).displaySubmittedPath,
      '/workspace/agent-b',
    );
    expect(
      overridden.submissionStatus?.displaySummary,
      'Agent B session opened.',
    );
    expect(projection.hiddenIndexes, contains(3));
  });

  test('agent interaction projection hides mapped session status card', () {
    final openEnvelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'open missing cwd',
      detailText: 'send cwd',
    );
    final statusEnvelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'session',
        status: 'success',
        summary: 'Codex session opened for /workspace/demo.',
        detailText: 'Workspace: /workspace/demo\nWorker: starting',
        referenceId: 'session-1',
      ),
    );

    final projection = ChatAgentInteractionCardProjector.project([
      MessageModel(
        msgId: 'm1',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1000,
        content: openEnvelope.content,
        extra: openEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm2',
        sessionId: 's1',
        senderId: 'u-1',
        senderType: 1,
        createdAt: 1100,
        content: 'grix://open/session?cwd=%2Fworkspace%2Fdemo',
      ),
      MessageModel(
        msgId: 'm3',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1200,
        content: statusEnvelope.content,
        extra: statusEnvelope.extra,
      ),
    ]);

    final overridden = projection.overridesByIndex[0];
    expect(projection.hiddenIndexes, contains(2));
    expect(overridden, isA<ChatAgentOpenSessionCardData>());
    expect(
      (overridden as ChatAgentOpenSessionCardData).displaySubmittedPath,
      '/workspace/demo',
    );
    expect(overridden.submissionStatus, isNotNull);
    expect(
      overridden.submissionStatus!.displaySummary,
      'Codex session opened for /workspace/demo.',
    );
  });

  test('agent interaction projection keeps only the latest retry status', () {
    final openEnvelope = ChatMessageCardCodec.buildAgentOpenSessionCard(
      summaryText: 'open missing cwd',
      detailText: 'send cwd',
    );
    final genericErrorEnvelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'session',
        status: 'error',
        summary: 'Codex session could not be opened.',
        detailText: 'Local service request failed with status 500',
        referenceId: 'session-1',
      ),
    );
    final detailErrorEnvelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'session',
        status: 'error',
        summary: 'Codex session could not be opened.',
        detailText: 'Directory does not exist: /eee',
        referenceId: 'session-1',
      ),
    );
    final successEnvelope = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'session',
        status: 'success',
        summary: 'Codex session opened for /workspace/demo.',
        detailText: 'Workspace: /workspace/demo\nWorker: starting',
        referenceId: 'session-1',
      ),
    );

    final projection = ChatAgentInteractionCardProjector.project([
      MessageModel(
        msgId: 'm1',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1000,
        content: openEnvelope.content,
        extra: openEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm2',
        sessionId: 's1',
        senderId: 'u-1',
        senderType: 1,
        createdAt: 1100,
        content: 'grix://open/session?cwd=%2Feee',
      ),
      MessageModel(
        msgId: 'm3',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1200,
        content: genericErrorEnvelope.content,
        extra: genericErrorEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm4',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1300,
        content: detailErrorEnvelope.content,
        extra: detailErrorEnvelope.extra,
      ),
      MessageModel(
        msgId: 'm5',
        sessionId: 's1',
        senderId: 'u-1',
        senderType: 1,
        createdAt: 1400,
        content: 'grix://open/session?cwd=%2Fworkspace%2Fdemo',
      ),
      MessageModel(
        msgId: 'm6',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1500,
        content: successEnvelope.content,
        extra: successEnvelope.extra,
      ),
    ]);

    final overridden = projection.overridesByIndex[0];
    expect(overridden, isA<ChatAgentOpenSessionCardData>());
    expect(
      (overridden as ChatAgentOpenSessionCardData).displaySubmittedPath,
      '/workspace/demo',
    );
    expect(overridden.submissionStatus, isNotNull);
    expect(overridden.submissionStatus!.displayStatus, 'success');
    expect(
      overridden.submissionStatus!.displaySummary,
      'Codex session opened for /workspace/demo.',
    );
    expect(projection.hiddenIndexes, containsAll(<int>[2, 3, 5]));
  });

  test('agent interaction projection maps question reply back to card', () {
    const questionCard = ChatAgentQuestionCardData(
      requestId: 'req-question-1',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose environment.',
          options: ['prod', 'staging'],
        ),
      ],
    );
    final envelope = ChatMessageCardCodec.encode(questionCard);

    final projection = ChatAgentInteractionCardProjector.project([
      MessageModel(
        msgId: 'm1',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1000,
        content: envelope.content,
        extra: envelope.extra,
      ),
      MessageModel(
        msgId: 'm2',
        sessionId: 's1',
        senderId: 'u-1',
        senderType: 1,
        createdAt: 1100,
        content: ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(
          questionCard,
          'staging',
        ),
      ),
    ]);

    final overridden = projection.overridesByIndex[0];
    expect(overridden, isA<ChatAgentQuestionCardData>());
    expect(
      (overridden as ChatAgentQuestionCardData).displaySubmittedAnswer,
      'staging',
    );
    expect(overridden.submissionStatus, isNull);
  });

  test('agent interaction projection keeps latest question retry result', () {
    const questionCard = ChatAgentQuestionCardData(
      requestId: 'req-question-1',
      questions: [
        ChatAgentQuestionPrompt(
          index: 1,
          header: 'Environment',
          prompt: 'Choose environment.',
          options: ['prod', 'staging'],
        ),
      ],
    );
    final envelope = ChatMessageCardCodec.encode(questionCard);
    final errorStatus = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'question',
        status: 'error',
        summary: 'Question request req-question-1 could not be recorded.',
        detailText: 'The reply format is invalid.',
        referenceId: 'req-question-1',
      ),
    );
    final successStatus = ChatMessageCardCodec.encode(
      const ChatAgentStatusCardData(
        category: 'question',
        status: 'success',
        summary: 'Question request req-question-1 answers recorded.',
        referenceId: 'req-question-1',
      ),
    );

    final projection = ChatAgentInteractionCardProjector.project([
      MessageModel(
        msgId: 'm1',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1000,
        content: envelope.content,
        extra: envelope.extra,
      ),
      MessageModel(
        msgId: 'm2',
        sessionId: 's1',
        senderId: 'u-1',
        senderType: 1,
        createdAt: 1100,
        content: ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(
          questionCard,
          'staging',
        ),
      ),
      MessageModel(
        msgId: 'm3',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1200,
        content: errorStatus.content,
        extra: errorStatus.extra,
      ),
      MessageModel(
        msgId: 'm4',
        sessionId: 's1',
        senderId: 'u-1',
        senderType: 1,
        createdAt: 1300,
        content: ChatAgentCardActionEncoder.buildQuestionQuickReplyAction(
          questionCard,
          'prod',
        ),
      ),
      MessageModel(
        msgId: 'm5',
        sessionId: 's1',
        senderId: 'agent-1',
        senderType: 2,
        createdAt: 1400,
        content: successStatus.content,
        extra: successStatus.extra,
      ),
    ]);

    final overridden = projection.overridesByIndex[0];
    expect(overridden, isA<ChatAgentQuestionCardData>());
    expect(
      (overridden as ChatAgentQuestionCardData).displaySubmittedAnswer,
      'prod',
    );
    expect(overridden.submissionStatus, isNotNull);
    expect(overridden.submissionStatus!.displayStatus, 'success');
    expect(
      overridden.submissionStatus!.displaySummary,
      'Question request req-question-1 answers recorded.',
    );
    expect(projection.hiddenIndexes, containsAll(<int>[2, 4]));
  });
}
