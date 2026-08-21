import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // 验证：getLastMessages 逐会话取"最新一条可预览消息"的行为契约。
  // 该查询由全表 GROUP BY 聚合改写为逐会话索引查询后，语义必须保持不变：
  // 排除流式占位（msg_type=4）与纯卡片消息，同时间戳取 msg_id 更大者，
  // 只存在消息、没有 sessions 行的会话同样要出现在结果里。
  test('getLastMessages returns latest previewable message per session', () async {
    final userId = 'last-messages-${DateTime.now().microsecondsSinceEpoch}';
    await LocalDb.initDatabaseFactory();
    await LocalDb.setActiveUser(userId);
    try {
      await LocalDb.batchInsertMessages([
        // s1: 普通会话，最新一条是文本。
        {
          'msg_id': '1001',
          'session_id': 's1',
          'sender_id': 'u1',
          'sender_type': 1,
          'msg_type': 1,
          'content': '旧消息',
          'created_at': 1000,
        },
        {
          'msg_id': '1002',
          'session_id': 's1',
          'sender_id': 'u1',
          'sender_type': 1,
          'msg_type': 1,
          'content': '新消息',
          'created_at': 2000,
        },
        // s2: 最新一条是卡片消息，应回退到上一条可预览文本。
        {
          'msg_id': '2001',
          'session_id': 's2',
          'sender_id': 'a1',
          'sender_type': 2,
          'msg_type': 1,
          'content': '可预览回复',
          'created_at': 1000,
        },
        {
          'msg_id': '2002',
          'session_id': 's2',
          'sender_id': 'a1',
          'sender_type': 2,
          'msg_type': 1,
          'content': '[工具执行](grix://card/tool_execution?id=1)',
          'created_at': 2000,
        },
        // s3: 只有流式占位与卡片，无可预览消息，不应出现在结果里。
        {
          'msg_id': '3001',
          'session_id': 's3',
          'sender_id': 'a1',
          'sender_type': 2,
          'msg_type': 4,
          'content': '',
          'created_at': 1000,
        },
        {
          'msg_id': '3002',
          'session_id': 's3',
          'sender_id': 'a1',
          'sender_type': 2,
          'msg_type': 1,
          'content': '[思考](grix://card/thinking?id=2)',
          'created_at': 2000,
        },
        // s4: 同一 created_at 两条，取 msg_id 更大者。
        {
          'msg_id': '4001',
          'session_id': 's4',
          'sender_id': 'u1',
          'sender_type': 1,
          'msg_type': 1,
          'content': '同秒较早',
          'created_at': 5000,
        },
        {
          'msg_id': '4002',
          'session_id': 's4',
          'sender_id': 'u1',
          'sender_type': 1,
          'msg_type': 1,
          'content': '同秒较晚',
          'created_at': 5000,
        },
        // s5: 最新一条是正文+卡片，应作为可预览消息，不能回退到更早的错误文本。
        {
          'msg_id': '5001',
          'session_id': 's5',
          'sender_id': 'a1',
          'sender_type': 2,
          'msg_type': 1,
          'content': 'connection failed',
          'created_at': 1000,
        },
        {
          'msg_id': '5002',
          'session_id': 's5',
          'sender_id': 'a1',
          'sender_type': 2,
          'msg_type': 1,
          'content': '已修好登录\n[文件](grix://card/file?path=app.go)',
          'created_at': 2000,
        },
        {
          'msg_id': '5003',
          'session_id': 's5',
          'sender_id': 'a1',
          'sender_type': 2,
          'msg_type': 1,
          'status': 'error',
          'content': 'stream failed',
          'created_at': 3000,
        },
      ]);

      final lastBySession = await LocalDb.getLastMessages();

      expect(lastBySession['s1']?['msg_id'], '1002');
      expect(lastBySession['s1']?['content'], '新消息');
      expect(lastBySession['s2']?['msg_id'], '2001');
      expect(lastBySession.containsKey('s3'), isFalse);
      expect(lastBySession['s4']?['msg_id'], '4002');
      expect(lastBySession['s5']?['msg_id'], '5002');
      expect(
        lastBySession['s5']?['content'],
        '已修好登录\n[文件](grix://card/file?path=app.go)',
      );
    } finally {
      await LocalDb.setActiveUser(null);
    }
  });
}
