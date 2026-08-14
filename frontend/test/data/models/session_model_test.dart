import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/session_model.dart';

void main() {
  test(
    'compareByPriority keeps pinned sessions dynamic by latest activity',
    () {
      final now = DateTime.now().millisecondsSinceEpoch;
      final newerPinned = SessionModel(
        sessionId: 'pinned-new',
        updatedAt: now,
        isPinned: true,
        pinnedAt: now - 2000,
        lastMessageTime: now,
      );
      final olderPinned = SessionModel(
        sessionId: 'pinned-old',
        updatedAt: now - 1000,
        isPinned: true,
        pinnedAt: now,
        lastMessageTime: now - 1000,
      );

      final sessions = [olderPinned, newerPinned]
        ..sort(SessionModel.compareByPriority);

      expect(sessions.first.sessionId, 'pinned-new');
    },
  );
}
