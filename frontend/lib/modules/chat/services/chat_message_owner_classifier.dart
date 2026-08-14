import '../../../data/models/message_model.dart';

class ChatMessageOwnerClassifier {
  const ChatMessageOwnerClassifier._();

  static bool isMineMessage(MessageModel message, {String? currentUserId}) {
    if (message.senderType == 2) {
      return false;
    }
    final normalizedCurrentUserId = currentUserId?.trim() ?? '';
    final normalizedSenderId = message.senderId.trim();
    return normalizedSenderId == 'me' ||
        (normalizedCurrentUserId.isNotEmpty &&
            normalizedSenderId == normalizedCurrentUserId);
  }

  static String ownerKeyForMessage(
    MessageModel message, {
    String? currentUserId,
  }) {
    return ownerKey(
      senderId: message.senderId,
      senderType: message.senderType,
      isMine: isMineMessage(message, currentUserId: currentUserId),
      currentUserId: currentUserId,
    );
  }

  static bool isSameOwner(
    MessageModel left,
    MessageModel right, {
    String? currentUserId,
  }) {
    return ownerKeyForMessage(left, currentUserId: currentUserId) ==
        ownerKeyForMessage(right, currentUserId: currentUserId);
  }

  static String visualSeed({
    required String senderId,
    required int senderType,
    required bool isMine,
    String? currentUserId,
  }) {
    if (senderType == 1) {
      if (isMine) {
        final myId = currentUserId?.trim() ?? '';
        if (myId.isNotEmpty) {
          return myId;
        }
      }
      final normalizedSenderId = senderId.trim();
      if (normalizedSenderId.isNotEmpty) {
        return normalizedSenderId;
      }
    }

    return ownerKey(
      senderId: senderId,
      senderType: senderType,
      isMine: isMine,
      currentUserId: currentUserId,
    );
  }

  static String ownerKey({
    required String senderId,
    required int senderType,
    required bool isMine,
    String? currentUserId,
  }) {
    final normalizedSenderId = senderId.trim();
    if (isMine) {
      final normalizedCurrentUserId = currentUserId?.trim() ?? '';
      final stableSelfId = normalizedCurrentUserId.isNotEmpty
          ? normalizedCurrentUserId
          : (normalizedSenderId.isNotEmpty ? normalizedSenderId : 'me');
      return 'self:human:$stableSelfId';
    }

    final ownerType = _ownerType(senderType);
    final stableSenderId = normalizedSenderId.isNotEmpty
        ? normalizedSenderId
        : 'unknown';
    return '$ownerType:$stableSenderId';
  }

  static String _ownerType(int senderType) {
    if (senderType == 2) {
      return 'agent';
    }
    if (senderType == 1) {
      return 'human';
    }
    return 'type:$senderType';
  }
}
