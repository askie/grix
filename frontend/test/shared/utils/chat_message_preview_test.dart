import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/chat_message_preview.dart';

void main() {
  test('isStandaloneCardMessage only matches a whole-message card', () {
    expect(
      ChatMessagePreview.isStandaloneCardMessage(
        '[工具执行](grix://card/tool_execution?id=1)',
      ),
      isTrue,
    );
    expect(
      ChatMessagePreview.isStandaloneCardMessage(
        '已修好登录\n[文件](grix://card/file?path=app.go)',
      ),
      isFalse,
    );
  });

  test('summarize keeps plain text stable', () {
    expect(ChatMessagePreview.summarize('  hello   world  '), 'hello world');
  });

  test('summarize converts markdown image into placeholder', () {
    expect(
      ChatMessagePreview.summarize('![image](https://example.com/demo.png)'),
      '[image]',
    );
  });

  test('summarize strips markdown formatting and preserves link text', () {
    expect(
      ChatMessagePreview.summarize('**Hello** [world](https://example.com)'),
      'Hello world',
    );
  });

  test('summarize converts mermaid fence into diagram placeholder', () {
    expect(
      ChatMessagePreview.summarize(
        'Here is a flowchart\n```mermaid\ngraph TD\nA --> B\n```',
      ),
      'Here is a flowchart [diagram]',
    );
  });

  test(
    'summarize converts uppercase mermaid fence into diagram placeholder',
    () {
      expect(
        ChatMessagePreview.summarize(
          'Here is a flowchart\n```Mermaid\ngraph TD\nA --> B\n```',
        ),
        'Here is a flowchart [diagram]',
      );
    },
  );

  test('summarize extracts text from structured json content', () {
    expect(
      ChatMessagePreview.summarize(
        '{"type":"assistant","content":"收到！ ✅测试正常～ 还需要测试什么吗？"}',
      ),
      '收到！ ✅测试正常～ 还需要测试什么吗？',
    );
  });

  test('summarizeTitle keeps underscores in snake_case identifiers', () {
    expect(
      ChatMessagePreview.summarizeTitle('session_agent_states'),
      'session_agent_states',
    );
    expect(
      ChatMessagePreview.summarizeTitle(
        'rename agent_task_query to chat_state_query',
      ),
      'rename agent_task_query to chat_state_query',
    );
    expect(
      ChatMessagePreview.summarizeTitle('__init__ and __name__'),
      '__init__ and __name__',
    );
  });

  test('summarizeTitle keeps spaces between english words', () {
    expect(
      ChatMessagePreview.summarizeTitle('Fix CI self hosted runner'),
      'Fix CI self hosted runner',
    );
  });

  test('summarizeTitle still strips paired bold markers', () {
    expect(
      ChatMessagePreview.summarizeTitle('**Deploy** the build'),
      'Deploy the build',
    );
  });

  test('summarize still strips underscores (message preview unchanged)', () {
    expect(
      ChatMessagePreview.summarize('session_agent_states'),
      'sessionagentstates',
    );
  });

  // 会话列表预览是主线程热点：缓存让相同原文重复调用拿到一致结果（避免每次重建重算）。
  test('summarize is idempotent / memo-stable for repeated input', () {
    const raw = '**Hello** [world](https://example.com)';
    final first = ChatMessagePreview.summarize(raw);
    final second = ChatMessagePreview.summarize(raw);
    expect(second, first);
    expect(second, 'Hello world');
  });

  // 防 ANR 回归：单条巨型 / 病态 HTML 输入不得卡死，必须有界返回（截断保护）。
  // 没有截断时，下面这种超长无闭合 `<img` 会触发正则灾难性回溯把主线程顶死。
  test('summarize handles a huge pathological input quickly and safely', () {
    final huge = '<img ${'a' * 200000}';
    final sw = Stopwatch()..start();
    final result = ChatMessagePreview.summarize(huge);
    sw.stop();
    expect(result, isA<String>());
    // 截断到前 4000 字符处理，正常应是毫秒级；给足余量防 CI 抖动。
    expect(sw.elapsedMilliseconds, lessThan(1000));
  });

  group('selectSessionPreview', () {
    test('uses the stream when it is newer than the last message', () {
      final preview = ChatMessagePreview.selectSessionPreview(
        lastMessage: '上一条消息',
        lastMessageTime: 1000,
        streamingText: '最新流式回复',
        streamingUpdatedAt: 2000,
      );

      expect(preview.text, '最新流式回复');
      expect(preview.updatedAt, 2000);
    });

    test('uses a newer outgoing message over an older active stream', () {
      final preview = ChatMessagePreview.selectSessionPreview(
        lastMessage: '我随后发出的回复',
        lastMessageTime: 3000,
        streamingText: '正在流式输出的旧回复',
        streamingUpdatedAt: 2000,
      );

      expect(preview.text, '我随后发出的回复');
      expect(preview.updatedAt, 3000);
    });

    test('uses the stream when there is no last message', () {
      final preview = ChatMessagePreview.selectSessionPreview(
        lastMessage: '',
        lastMessageTime: 0,
        streamingText: '第一条流式回复',
        streamingUpdatedAt: 1000,
      );

      expect(preview.text, '第一条流式回复');
      expect(preview.updatedAt, 1000);
    });
  });
}
