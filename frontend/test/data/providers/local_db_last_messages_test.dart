import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';
import 'package:grix/shared/utils/chat_message_preview.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // 验证：getLastMessages 逐会话取"最新一条可预览消息"的行为契约。
  // 该查询由全表 GROUP BY 聚合改写为逐会话索引查询后，语义必须保持不变：
  // 排除流式占位（msg_type=4）与纯卡片消息，同时间戳取 msg_id 更大者，
  // 只存在消息、没有 sessions 行的会话同样要出现在结果里。
  test(
    'getLastMessages returns latest previewable message per session',
    () async {
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
    },
  );

  // 验证：整表重载走平台通道的结果只带摘要用到的列，长正文按字符截前
  // lastMessagePreviewMaxChars 个；选中的行与逐会话取整行（改前 SELECT m.*
  // 取到的同一行）一一对应，行数不变，一行摘要不受截断影响。
  test(
    'getLastMessages projects preview columns and clamps long content',
    () async {
      final userId =
          'last-messages-projection-${DateTime.now().microsecondsSinceEpoch}';
      await LocalDb.initDatabaseFactory();
      await LocalDb.setActiveUser(userId);
      try {
        const maxChars = LocalDbSessionRepository.lastMessagePreviewMaxChars;
        // 中文与 emoji（UTF-16 代理对）混排：截断按字符计，不能劈开代理对。
        final longContent = List.filled(maxChars, '摘要😀').join();
        const baseTime = 1758600000000;
        Map<String, dynamic> msg(
          String msgId,
          String sessionId,
          String content,
          int createdAt, {
          int msgType = 1,
          String? status,
        }) => {
          'msg_id': msgId,
          'session_id': sessionId,
          'sender_id': 'a1',
          'sender_type': 2,
          'msg_type': msgType,
          'content': content,
          'extra': '{"stream_id":"s-$msgId"}',
          'quoted_message_id': 'q-$msgId',
          'status': status,
          'agent_delivery_status': 'delivered',
          'local_seq': 'ls-$msgId',
          'inbox_seq': createdAt,
          'visible_to': '["u1"]',
          'created_at': createdAt,
        };
        await LocalDb.batchInsertMessages([
          msg('p1-1', 'p1', '更早的短消息', baseTime),
          msg('p1-2', 'p1', longContent, baseTime + 1),
          msg('p2-1', 'p2', '短消息', baseTime + 2),
          msg('p2-2', 'p2', '', baseTime + 3, msgType: 4),
          msg('p3-1', 'p3', '[思考](grix://card/thinking?id=1)', baseTime + 4),
          msg('p4-1', 'p4', '回复', baseTime + 5),
          msg('p4-2', 'p4', 'stream failed', baseTime + 6, status: 'error'),
        ]);

        final lastBySession = await LocalDb.getLastMessages();

        final fullBySession = <String, Map<String, dynamic>>{};
        for (final sid in const ['p1', 'p2', 'p3', 'p4']) {
          final row = await LocalDb.getLatestPreviewableMessage(sid);
          if (row != null) fullBySession[sid] = row;
        }
        expect(lastBySession.keys.toSet(), {'p1', 'p2', 'p4'});
        expect(lastBySession.keys.toSet(), fullBySession.keys.toSet());
        for (final entry in lastBySession.entries) {
          expect(entry.value.keys.toSet(), {
            'session_id',
            'msg_id',
            'created_at',
            'content',
          });
          expect(entry.value['session_id'], entry.key);
          expect(entry.value['msg_id'], fullBySession[entry.key]!['msg_id']);
          expect(entry.value['created_at'], isA<int>());
        }
        expect(lastBySession['p2']!['content'], '短消息');
        expect(lastBySession['p4']!['content'], '回复');

        final clamped = lastBySession['p1']!['content'] as String;
        expect(fullBySession['p1']!['content'], longContent);
        expect(longContent.runes.length, greaterThan(maxChars));
        expect(clamped.runes.length, maxChars);
        expect(clamped, String.fromCharCodes(longContent.runes.take(maxChars)));

        String firstLine(String raw) => String.fromCharCodes(
          ChatMessagePreview.summarize(raw).runes.take(60),
        );
        expect(firstLine(clamped), firstLine(longContent));
      } finally {
        await LocalDb.setActiveUser(null);
      }
    },
  );
}
