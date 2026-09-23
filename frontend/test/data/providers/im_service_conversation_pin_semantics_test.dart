import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';

class _RecordingImService extends ImService {
  final peerPinCalls = <Map<String, Object>>[];
  final sessionPinCalls = <Map<String, Object>>[];

  @override
  Future<bool> setPeerPinned({
    required String peerId,
    required List<String> sessionIds,
    required bool isPinned,
  }) async {
    peerPinCalls.add({
      'peerId': peerId,
      'sessionIds': List<String>.from(sessionIds)..sort(),
      'isPinned': isPinned,
    });
    return true;
  }

  @override
  Future<bool> setSessionPinned(
    String sessionId, {
    required bool isPinned,
  }) async {
    sessionPinCalls.add({'sessionId': sessionId, 'isPinned': isPinned});
    return true;
  }
}

SessionModel _session({
  required String id,
  required String type,
  String peerId = '',
  int peerType = 2,
  bool pinned = false,
  bool friendPinned = false,
}) {
  return SessionModel(
    sessionId: id,
    title: id,
    type: type,
    peerId: peerId,
    peerType: peerType,
    updatedAt: 1000,
    lastMessageTime: 1000,
    isPinned: pinned,
    pinnedAt: pinned ? 1000 : 0,
    friendIsPinned: friendPinned,
    friendPinnedAt: friendPinned ? 1000 : 0,
  );
}

void main() {
  late _RecordingImService imService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    imService = _RecordingImService();
  });

  tearDown(() {
    Get.reset();
  });

  test('private sessions read and write the peer-level pin', () async {
    imService.sessions.value = [
      _session(id: 'a1', type: 'private', peerId: 'agent-1', pinned: true),
      _session(id: 'a2', type: 'private', peerId: 'agent-1'),
      _session(id: 'b1', type: 'private', peerId: 'agent-2'),
    ];

    // A session-level pin alone never counts for a private conversation.
    expect(
      imService.isConversationPinnedForSession(imService.sessions[0]),
      isFalse,
    );
    expect(
      imService.isConversationPinnedForSession(
        _session(
          id: 'a3',
          type: 'private',
          peerId: 'agent-1',
          friendPinned: true,
        ),
      ),
      isTrue,
    );

    expect(
      imService.conversationPinnedAtForSession(
        _session(
          id: 'a3',
          type: 'private',
          peerId: 'agent-1',
          friendPinned: true,
        ),
      ),
      1000,
    );
    expect(imService.conversationPinnedAtForSession(imService.sessions[0]), 0);

    final ok = await imService.setConversationPinnedForSession(
      'a2',
      isPinned: true,
    );
    expect(ok, isTrue);
    expect(imService.sessionPinCalls, isEmpty);
    expect(imService.peerPinCalls, [
      {
        'peerId': 'agent-1',
        'sessionIds': ['a1', 'a2'],
        'isPinned': true,
      },
    ]);
  });

  test('group sessions keep the session-level pin', () async {
    imService.sessions.value = [
      _session(id: 'g1', type: 'group', pinned: true),
    ];
    expect(
      imService.isConversationPinnedForSession(imService.sessions[0]),
      isTrue,
    );
    final ok = await imService.setConversationPinnedForSession(
      'g1',
      isPinned: false,
    );
    expect(ok, isTrue);
    expect(imService.peerPinCalls, isEmpty);
    expect(imService.sessionPinCalls, [
      {'sessionId': 'g1', 'isPinned': false},
    ]);
  });

  test('private session without peer identity is not downgraded', () async {
    imService.sessions.value = [_session(id: 'p1', type: 'private')];
    final ok = await imService.setConversationPinnedForSession(
      'p1',
      isPinned: true,
    );
    expect(ok, isFalse);
    expect(imService.peerPinCalls, isEmpty);
    expect(imService.sessionPinCalls, isEmpty);
  });
}
