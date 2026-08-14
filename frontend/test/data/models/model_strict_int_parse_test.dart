import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/shared/models/session_avatar_member.dart';

class _IntegerLikeValue {
  const _IntegerLikeValue(this.raw);

  final String raw;

  @override
  String toString() => raw;
}

void main() {
  group('MessageModel strict int parse', () {
    test('parses when int fields are valid', () {
      final m = MessageModel.fromJson({
        'msg_id': '1',
        'session_id': 's1',
        'sender_id': '1001',
        'sender_type': 1,
        'msg_type': 1,
        'content': 'hello',
        'created_at': 1700000000,
      });

      expect(m.msgType, 1);
      expect(m.senderType, 1);
      expect(m.createdAt, 1700000000 * 1000);
    });

    test('accepts numeric string and normalizes to int', () {
      final m = MessageModel.fromJson({
        'msg_id': '1',
        'session_id': 's1',
        'sender_id': '1001',
        'msg_type': '1',
        'created_at': 1700000000,
      });
      expect(m.msgType, 1);
    });

    test('throws when msg_type is non-numeric string', () {
      expect(
        () => MessageModel.fromJson({
          'msg_id': '1',
          'session_id': 's1',
          'sender_id': '1001',
          'msg_type': 'one',
          'created_at': 1700000000,
        }),
        throwsFormatException,
      );
    });

    test('accepts integral num and rejects fractional num', () {
      final ok = MessageModel.fromJson({
        'msg_id': '2',
        'session_id': 's1',
        'sender_id': '1001',
        'msg_type': 1.0,
        'created_at': 1700000000.0,
      });
      expect(ok.msgType, 1);
      expect(ok.createdAt, 1700000000 * 1000);

      expect(
        () => MessageModel.fromJson({
          'msg_id': '3',
          'session_id': 's1',
          'sender_id': '1001',
          'msg_type': 1.5,
          'created_at': 1700000000,
        }),
        throwsFormatException,
      );
    });

    test('accepts BigInt integer fields from web sqlite', () {
      final m = MessageModel.fromJson({
        'msg_id': '4',
        'session_id': 's1',
        'sender_id': '1001',
        'msg_type': BigInt.one,
        'created_at': BigInt.from(1700000000),
      });

      expect(m.msgType, 1);
      expect(m.createdAt, 1700000000 * 1000);
    });

    test('normalizes integral numeric identifiers from web json', () {
      final m = MessageModel.fromJson({
        'msg_id': 123.0,
        'session_id': 's1',
        'sender_id': 1001.0,
        'msg_type': 1,
        'created_at': 1700000000,
      });

      expect(m.msgId, '123');
      expect(m.senderId, '1001');
    });
  });

  group('SessionModel strict int parse', () {
    test('accepts numeric string and normalizes to int', () {
      final s = SessionModel.fromJson({
        'session_id': 's1',
        'updated_at': '1700000000',
        'unread_count': 0,
        'last_message_time': 1700000000000,
      });
      expect(s.updatedAt, 1700000000);
    });

    test('throws when updated_at is non-numeric string', () {
      expect(
        () => SessionModel.fromJson({
          'session_id': 's1',
          'updated_at': 'bad',
          'unread_count': 0,
          'last_message_time': 1700000000000,
        }),
        throwsFormatException,
      );
    });

    test('accepts integral num and rejects fractional num', () {
      final ok = SessionModel.fromJson({
        'session_id': 's1',
        'updated_at': 1700000000000.0,
        'is_pinned': 1,
        'is_muted': 1,
        'pinned_at': 1700000000000.0,
        'unread_count': 0.0,
        'last_message_time': 1700000000000.0,
      });
      expect(ok.updatedAt, 1700000000000);
      expect(ok.isPinned, isTrue);
      expect(ok.isMuted, isTrue);
      expect(ok.pinnedAt, 1700000000000);

      expect(
        () => SessionModel.fromJson({
          'session_id': 's1',
          'updated_at': 1700000000000.1,
          'unread_count': 0,
          'last_message_time': 1700000000000,
        }),
        throwsFormatException,
      );
    });

    test('accepts BigInt session timestamps and flags', () {
      final s = SessionModel.fromJson({
        'session_id': 's1',
        'updated_at': BigInt.from(1700000000000),
        'is_pinned': BigInt.one,
        'is_muted': BigInt.zero,
        'pinned_at': BigInt.from(1700000000000),
        'unread_count': BigInt.zero,
        'last_message_time': BigInt.from(1700000000000),
      });

      expect(s.updatedAt, 1700000000000);
      expect(s.isPinned, isTrue);
      expect(s.isMuted, isFalse);
      expect(s.pinnedAt, 1700000000000);
    });

    test('accepts integer-like session timestamps and flags', () {
      final s = SessionModel.fromJson({
        'session_id': 's1',
        'updated_at': const _IntegerLikeValue('1700000000000'),
        'is_pinned': const _IntegerLikeValue('1'),
        'is_muted': const _IntegerLikeValue('0'),
        'pinned_at': const _IntegerLikeValue('1700000000000'),
        'unread_count': const _IntegerLikeValue('0'),
        'last_message_time': const _IntegerLikeValue('1700000000000'),
      });

      expect(s.updatedAt, 1700000000000);
      expect(s.isPinned, isTrue);
      expect(s.isMuted, isFalse);
      expect(s.pinnedAt, 1700000000000);
    });

    test('throws when is_pinned is invalid', () {
      expect(
        () => SessionModel.fromJson({
          'session_id': 's1',
          'updated_at': 1700000000000,
          'is_pinned': 'yes',
          'unread_count': 0,
          'last_message_time': 1700000000000,
        }),
        throwsFormatException,
      );
    });

    test('throws when is_muted is invalid', () {
      expect(
        () => SessionModel.fromJson({
          'session_id': 's1',
          'updated_at': 1700000000000,
          'is_muted': 'disabled',
          'unread_count': 0,
          'last_message_time': 1700000000000,
        }),
        throwsFormatException,
      );
    });

    test('parses and serializes cached group avatar members', () {
      final s = SessionModel.fromJson({
        'session_id': 'g1',
        'type': 'group',
        'updated_at': 1700000000000,
        'last_message_time': 1700000000000,
        'group_avatar_members': jsonEncode([
          {
            'member_id': '1001',
            'member_type': 1,
            'display_name': '测试A',
            'avatar_url': 'https://example.com/a.png',
          },
          {
            'member_id': '1002',
            'member_type': '2',
            'display_name': 'Agent B',
            'avatar_url': '',
          },
        ]),
      });

      expect(s.cachedGroupAvatarMembers, hasLength(2));
      expect(
        s.cachedGroupAvatarMembers.first,
        const SessionAvatarMember(
          memberId: '1001',
          memberType: 1,
          displayName: '测试A',
          avatarUrl: 'https://example.com/a.png',
        ),
      );

      final encoded = s.toJson()['group_avatar_members'];
      expect(encoded, isA<String>());
      final decoded = jsonDecode(encoded as String) as List<dynamic>;
      expect(decoded, hasLength(2));
      expect(decoded.first['member_id'], '1001');
    });
  });
}
