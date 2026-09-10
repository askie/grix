import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/local_search_result.dart';
import 'package:grix/data/providers/local_db.dart';

const String _testUserId = 'search-rank-test-user';

Future<void> _seedSession({
  required String sessionId,
  required String title,
  required String lastMessage,
  required int updatedAt,
  String type = 'group',
  String peerId = '',
  int peerType = 0,
}) {
  return LocalDb.upsertSession({
    'session_id': sessionId,
    'title': title,
    'type': type,
    'peer_id': peerId,
    'peer_type': peerType,
    'peer_nickname': '',
    'peer_username': '',
    'updated_at': updatedAt,
    'unread_count': 0,
    'last_message': lastMessage,
    'last_message_time': updatedAt,
  });
}

Future<void> _seedMessage({
  required String msgId,
  required String sessionId,
  required String content,
  required int createdAt,
}) {
  return LocalDb.batchUpsertMessages([
    {
      'msg_id': msgId,
      'session_id': sessionId,
      'sender_id': 'u1',
      'sender_type': 1,
      'msg_type': 1,
      'content': content,
      'status': 'sent',
      'created_at': createdAt,
    },
  ]);
}

void main() {
  var dbAvailable = true;

  setUpAll(() async {
    try {
      await LocalDb.setActiveUser(_testUserId);
    } catch (e) {
      dbAvailable = false;
      // ignore: avoid_print
      print('LocalDb unavailable in this env: $e');
    }
  });

  tearDownAll(() async {
    if (!dbAvailable) return;
    await LocalDb.clearActiveUserData();
    await LocalDb.setActiveUser(null);
  });

  setUp(() async {
    if (!dbAvailable) return;
    await LocalDb.clearActiveUserData();
  });

  test('会话搜索：两个词都命中的排在只命中一个的前面', () async {
    if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
    // 只命中「装修」的会话更新时间最新：纯 OR + updated_at 倒序会把它排在最前。
    await _seedSession(
      sessionId: 's-partial',
      title: '装修群',
      lastMessage: '',
      updatedAt: 9000,
    );
    await _seedSession(
      sessionId: 's-full',
      title: '老王装修',
      lastMessage: '',
      updatedAt: 1000,
    );
    await _seedSession(
      sessionId: 's-other',
      title: '老王',
      lastMessage: '',
      updatedAt: 8000,
    );

    final rows = await LocalDb.searchSessionRecords(['老王', '装修']);

    expect(
      rows.map((row) => row['session_id']).toList(),
      ['s-full', 's-partial', 's-other'],
      reason: '全部关键词命中的会话没有排在部分命中的前面',
    );
  });

  test('聊天记录搜索：两个词都命中的排在只命中一个的前面', () async {
    if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
    await _seedSession(
      sessionId: 's-1',
      title: '群',
      lastMessage: '',
      updatedAt: 1000,
    );
    await _seedMessage(
      msgId: 'm-partial',
      sessionId: 's-1',
      content: '装修的报价单发你了',
      createdAt: 9000,
    );
    await _seedMessage(
      msgId: 'm-full',
      sessionId: 's-1',
      content: '老王说装修下周开工',
      createdAt: 1000,
    );

    final messages = await LocalDb.searchMessages(['老王', '装修']);

    expect(
      messages.map((m) => m.msgId).toList(),
      ['m-full', 'm-partial'],
      reason: '全部关键词命中的消息没有排在部分命中的前面',
    );
  });

  test('单关键词仍按最近更新倒序，且会话与聊天记录都能命中', () async {
    if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
    await _seedSession(
      sessionId: 's-old',
      title: '装修旧群',
      lastMessage: '',
      updatedAt: 1000,
    );
    await _seedSession(
      sessionId: 's-new',
      title: '装修新群',
      lastMessage: '',
      updatedAt: 9000,
    );
    await _seedMessage(
      msgId: 'm-1',
      sessionId: 's-new',
      content: '装修进度同步',
      createdAt: 5000,
    );

    final result = await LocalDb.search(['装修']);

    expect(result.matchedSessions.map((s) => s.sessionId).toList(), [
      's-new',
      's-old',
    ]);
    expect(result.matchedMessages.map((m) => m.msgId).toList(), ['m-1']);
  });

  test('结果按主键去重，AND 命中的行不会在 OR 补位时重复出现', () async {
    if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
    await _seedSession(
      sessionId: 's-full',
      title: '老王装修',
      lastMessage: '',
      updatedAt: 1000,
    );

    final rows = await LocalDb.searchSessionRecords(['老王', '装修']);

    expect(rows.map((row) => row['session_id']).toList(), ['s-full']);
  });

  test('isCancelled 为真时直接跳过查询，不返回命中（过期搜索不再跑 SQL）', () async {
    if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
    await _seedSession(
      sessionId: 's-cancel',
      title: '装修群',
      lastMessage: '',
      updatedAt: 1000,
    );

    final cancelled = await LocalDb.searchSessionRecords(
      ['装修'],
      isCancelled: () => true,
    );
    expect(cancelled, isEmpty, reason: 'isCancelled 为真时不应该还跑 SQL 返回命中');

    // 对照组：同样的数据不带 isCancelled 应该能命中，证明上面的空结果
    // 确实来自跳过而不是数据本身没写进去。
    final control = await LocalDb.searchSessionRecords(['装修']);
    expect(control, isNotEmpty);
  });

  test('isCancelled 在调用发起之后、真正轮到执行时才被读取', () async {
    if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
    await _seedSession(
      sessionId: 's-race',
      title: '装修群',
      lastMessage: '',
      updatedAt: 1000,
    );

    var isStale = false;
    // 发起调用的这一刻 isStale 还是 false；紧跟着同步地把它改成 true，
    // 模拟"连打时新一版搜索已经把这一版标记为过期"——因为 LocalDb 内部
    // 排队会先 await 一次才轮到执行，这个同步修改在它真正检查
    // isCancelled 之前就已经生效。
    final pending = LocalDb.searchSessionRecords(
      ['装修'],
      isCancelled: () => isStale,
    );
    isStale = true;
    final rows = await pending;

    expect(rows, isEmpty, reason: '轮到执行时已经过期，不应该再返回命中');
  });

  group('scope', () {
    test('peer 范围：全库有超过 limit 条其它会话同样命中关键词时，'
        '该 peer 自己的会话仍然能返回（修 200 上限截断）', () async {
      if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
      // 目标 peer 的会话故意用最旧的 updated_at 写入，配合极小 limit，
      // 验证的是「命中数 SQL 先按 scope 过滤」而不是「凑巧排到前面」。
      await _seedSession(
        sessionId: 's-target',
        title: '装修计划',
        lastMessage: '',
        updatedAt: 0,
        type: 'private',
        peerId: 'peer-1',
        peerType: 1,
      );
      for (var i = 0; i < 5; i++) {
        await _seedSession(
          sessionId: 's-noise-$i',
          title: '装修噪声$i',
          lastMessage: '',
          updatedAt: 10000 + i,
          type: 'private',
          peerId: 'peer-other-$i',
          peerType: 1,
        );
      }

      // limit=3：不加 scope 时全库排序会让目标会话（updated_at 最旧）被截掉。
      final unscoped = await LocalDb.searchSessionRecords(['装修'], limit: 3);
      expect(
        unscoped.map((row) => row['session_id']),
        isNot(contains('s-target')),
        reason: '对照组：不加 scope 时目标会话确实会被截断，证明下面不是巧合',
      );

      final scoped = await LocalDb.searchSessionRecords(
        ['装修'],
        limit: 3,
        scope: LocalSearchScope.peer(peerType: 1, peerId: 'peer-1'),
      );
      expect(
        scoped.map((row) => row['session_id']).toList(),
        ['s-target'],
        reason: 'scope 过滤在 SQL 里先做，不受全库其它会话占满 limit 名额影响',
      );
    });

    test('session 范围：只返回该 sessionId 的会话，即便标题相同', () async {
      if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
      await _seedSession(
        sessionId: 's-a',
        title: '同名会话',
        lastMessage: '',
        updatedAt: 1000,
        type: 'private',
        peerId: 'peer-2',
        peerType: 1,
      );
      await _seedSession(
        sessionId: 's-b',
        title: '同名会话',
        lastMessage: '',
        updatedAt: 2000,
        type: 'private',
        peerId: 'peer-2',
        peerType: 1,
      );

      final rows = await LocalDb.searchSessionRecords(
        ['同名'],
        scope: LocalSearchScope.session('s-a'),
      );

      expect(rows.map((row) => row['session_id']).toList(), ['s-a']);
    });

    test('消息搜索按 peer 范围：只命中该 peer 名下会话里的消息', () async {
      if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
      await _seedSession(
        sessionId: 's-mine',
        title: '我的会话',
        lastMessage: '',
        updatedAt: 1000,
        type: 'private',
        peerId: 'peer-3',
        peerType: 1,
      );
      await _seedSession(
        sessionId: 's-other',
        title: '别人的会话',
        lastMessage: '',
        updatedAt: 1000,
        type: 'private',
        peerId: 'peer-4',
        peerType: 1,
      );
      await _seedMessage(
        msgId: 'm-mine',
        sessionId: 's-mine',
        content: '装修报价单',
        createdAt: 1000,
      );
      await _seedMessage(
        msgId: 'm-other',
        sessionId: 's-other',
        content: '装修报价单',
        createdAt: 2000,
      );

      final messages = await LocalDb.searchMessages(
        ['装修'],
        scope: LocalSearchScope.peer(peerType: 1, peerId: 'peer-3'),
      );

      expect(messages.map((m) => m.msgId).toList(), ['m-mine']);
    });

    test('消息搜索按 session 范围：只命中该 sessionId 里的消息', () async {
      if (!dbAvailable) return markTestSkipped('LocalDb unavailable');
      await _seedSession(
        sessionId: 's-x',
        title: '会话X',
        lastMessage: '',
        updatedAt: 1000,
      );
      await _seedSession(
        sessionId: 's-y',
        title: '会话Y',
        lastMessage: '',
        updatedAt: 1000,
      );
      await _seedMessage(
        msgId: 'm-x',
        sessionId: 's-x',
        content: '进度同步',
        createdAt: 1000,
      );
      await _seedMessage(
        msgId: 'm-y',
        sessionId: 's-y',
        content: '进度同步',
        createdAt: 2000,
      );

      final messages = await LocalDb.searchMessages(
        ['进度'],
        scope: LocalSearchScope.session('s-x'),
      );

      expect(messages.map((m) => m.msgId).toList(), ['m-x']);
    });
  });
}
