import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';

const String _testUserId = 'search-rank-test-user';

Future<void> _seedSession({
  required String sessionId,
  required String title,
  required String lastMessage,
  required int updatedAt,
}) {
  return LocalDb.upsertSession({
    'session_id': sessionId,
    'title': title,
    'type': 'group',
    'peer_id': '',
    'peer_type': 0,
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
}
