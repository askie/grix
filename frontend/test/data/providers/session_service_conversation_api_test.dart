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
}
