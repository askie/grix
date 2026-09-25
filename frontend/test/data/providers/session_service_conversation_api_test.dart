import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/session_service.dart';

class _FakeAdapter implements HttpClientAdapter {
  int conversationRequests = 0;
  int createRequests = 0;
  Completer<void>? firstConversationGate;
  Completer<void>? firstConversationStarted;

  @override
  void close({bool force = false}) {}

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    if (options.uri.path == '/v1/sessions/conversations') {
      conversationRequests++;
      final requestNumber = conversationRequests;
      if (requestNumber == 1) {
        firstConversationStarted?.complete();
        await firstConversationGate?.future;
      }
      return _json({
        'code': 0,
        'data': {
          'list': [
            {
              'group_key': 'private:1:2001',
              'conversation_type': 'private',
              'latest_session_id': requestNumber == 1 ? 's-2001' : 's-2002',
              'title': 'Alice',
              'peer_id': '2001',
              'peer_type': 1,
              'latest_active_at': 1700000000000,
              'thread_count': 1,
            },
          ],
          'has_more': false,
          'next_cursor': '',
        },
      });
    }
    if (options.uri.path == '/v1/sessions/create') {
      createRequests++;
      return _json({
        'code': 0,
        'data': {'session_id': 'created-session'},
      });
    }
    if (options.uri.path == '/v1/sessions/list') {
      return _json({
        'code': 0,
        'data': {
          'list': [
            {
              'session_id': 's-1',
              'session_type': 1,
              'updated_at': 1700000000,
              'unread': 1,
              'last_msg': 'hello',
              'recent_messages': [
                {
                  'msg_id': '1234567890123456789',
                  'session_id': 's-1',
                  'sender_id': '2001',
                  'sender_type': 1,
                  'msg_type': 1,
                  'content': 'hello',
                  'created_at': 1700000000000,
                  'state_version': '7',
                },
                {'msg_id': '', 'created_at': 1700000000000},
              ],
            },
            {'session_id': 's-2', 'session_type': 1},
          ],
          'has_more': false,
          'cursor': 1700000000,
          'sync_head_cursor': 88,
        },
      });
    }
    return _json({'code': 404, 'msg': 'not found'}, status: 404);
  }
}

ResponseBody _json(Map<String, dynamic> body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

void main() {
  test('fetchConversationPage coalesces cached first-page requests', () async {
    final adapter = _FakeAdapter();
    final dio = Dio(
      BaseOptions(
        baseUrl: 'http://example.test/v1',
        validateStatus: (_) => true,
      ),
    )..httpClientAdapter = adapter;
    final service = SessionService.forTest(dio);

    final first = await service.fetchConversationPage(limit: 20);
    final second = await service.fetchConversationPage(limit: 20);

    expect(first.success, isTrue);
    expect(second.success, isTrue);
    expect(first.items.single.groupKey, 'private:1:2001');
    expect(second.items.single.groupKey, 'private:1:2001');
    expect(adapter.conversationRequests, 1);
  });

  test('createSession invalidates cached conversation first page', () async {
    final adapter = _FakeAdapter();
    final dio = Dio(
      BaseOptions(
        baseUrl: 'http://example.test/v1',
        validateStatus: (_) => true,
      ),
    )..httpClientAdapter = adapter;
    final service = SessionService.forTest(dio);

    await service.fetchConversationPage(limit: 20);
    expect(adapter.conversationRequests, 1);

    final sessionId = await service.createSession('2001', 1);
    expect(sessionId, 'created-session');
    expect(adapter.createRequests, 1);

    await service.fetchConversationPage(limit: 20);
    expect(adapter.conversationRequests, 2);
  });

  test(
    'invalidated in-flight first page does not refill stale cache',
    () async {
      final adapter = _FakeAdapter()
        ..firstConversationGate = Completer<void>()
        ..firstConversationStarted = Completer<void>();
      final dio = Dio(
        BaseOptions(
          baseUrl: 'http://example.test/v1',
          validateStatus: (_) => true,
        ),
      )..httpClientAdapter = adapter;
      final service = SessionService.forTest(dio);

      final staleFuture = service.fetchConversationPage(limit: 20);
      await adapter.firstConversationStarted!.future;
      expect(adapter.conversationRequests, 1);

      final sessionId = await service.createSession('2001', 1);
      expect(sessionId, 'created-session');

      final fresh = await service.fetchConversationPage(limit: 20);
      expect(fresh.items.single.latestSessionId, 's-2002');
      expect(adapter.conversationRequests, 2);

      adapter.firstConversationGate!.complete();
      final stale = await staleFuture;
      expect(stale.items.single.latestSessionId, 's-2001');

      final cached = await service.fetchConversationPage(limit: 20);
      expect(cached.items.single.latestSessionId, 's-2002');
      expect(adapter.conversationRequests, 2);
    },
  );

  test('sync_head snapshot parses recent_messages like history items', () async {
    final adapter = _FakeAdapter();
    final dio = Dio(
      BaseOptions(
        baseUrl: 'http://example.test/v1',
        validateStatus: (_) => true,
      ),
    )..httpClientAdapter = adapter;
    final service = SessionService.forTest(dio);

    final result = await service.fetchSyncV2BootstrapSnapshotsResult(limit: 10);
    expect(result.success, isTrue);
    expect(result.syncHeadCursor, 88);

    final withMessages = result.snapshots.singleWhere(
      (snapshot) => snapshot.sessionId == 's-1',
    );
    // The row with an empty msg_id is filtered out.
    expect(withMessages.recentMessages, hasLength(1));
    final message = withMessages.recentMessages.single;
    // int64 fields survive as strings (no 53-bit precision loss on Web).
    expect(message['msg_id'], '1234567890123456789');
    expect(message['state_version'], '7');
    expect(message['session_id'], 's-1');
    expect(message['created_at'], 1700000000000);

    // Snapshots without the field (old backends) stay empty.
    final withoutMessages = result.snapshots.singleWhere(
      (snapshot) => snapshot.sessionId == 's-2',
    );
    expect(withoutMessages.recentMessages, isEmpty);
  });
}
