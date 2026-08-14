import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/modules/chat/services/chat_message_owner_classifier.dart';

void main() {
  test('ChatMessageOwnerClassifier treats me and my user id as one owner', () {
    final optimistic = MessageModel(
      msgId: 'msg-me',
      sessionId: 'session-1',
      senderId: 'me',
      senderType: 1,
      createdAt: 1,
    );
    final confirmed = MessageModel(
      msgId: 'msg-user-id',
      sessionId: 'session-1',
      senderId: '1001',
      senderType: 1,
      createdAt: 2,
    );

    expect(
      ChatMessageOwnerClassifier.isMineMessage(
        optimistic,
        currentUserId: '1001',
      ),
      isTrue,
    );
    expect(
      ChatMessageOwnerClassifier.isSameOwner(
        optimistic,
        confirmed,
        currentUserId: '1001',
      ),
      isTrue,
    );
  });

  test(
    'ChatMessageOwnerClassifier separates agent and human with the same raw id',
    () {
      final agentMessage = MessageModel(
        msgId: 'msg-agent',
        sessionId: 'session-1',
        senderId: '42',
        senderType: 2,
        createdAt: 1,
      );
      final humanMessage = MessageModel(
        msgId: 'msg-human',
        sessionId: 'session-1',
        senderId: '42',
        senderType: 1,
        createdAt: 2,
      );

      expect(
        ChatMessageOwnerClassifier.isSameOwner(
          agentMessage,
          humanMessage,
          currentUserId: '1001',
        ),
        isFalse,
      );
      expect(
        ChatMessageOwnerClassifier.visualSeed(
          senderId: '42',
          senderType: 2,
          isMine: false,
        ),
        isNot(
          ChatMessageOwnerClassifier.visualSeed(
            senderId: '42',
            senderType: 1,
            isMine: false,
          ),
        ),
      );
    },
  );
}
