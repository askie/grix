import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/chat_message_content.dart';

void main() {
  test('unwrapStructuredText keeps plain text unchanged', () {
    expect(ChatMessageContent.unwrapStructuredText('hello'), 'hello');
  });

  test('unwrapStructuredText extracts content field from json', () {
    expect(
      ChatMessageContent.unwrapStructuredText(
        '{"type":"assistant","content":"**Hello** [OpenAI](https://openai.com)"}',
      ),
      '**Hello** [OpenAI](https://openai.com)',
    );
  });

  test('unwrapStructuredText preserves markdown fence layout from arrays', () {
    expect(
      ChatMessageContent.unwrapStructuredText(
        '{"content":["```mermaid\\n","flowchart TD\\n","A[开始] --> B[结束]\\n","```"]}',
      ),
      '```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```',
    );
  });

  test(
    'unwrapStructuredText preserves nested text nodes from structured arrays',
    () {
      expect(
        ChatMessageContent.unwrapStructuredText(
          '{"content":[{"type":"text","text":"```mermaid\\n"},{"type":"text","text":"flowchart TD\\nA[开始] --> B[结束]\\n```"}]}',
        ),
        '```mermaid\nflowchart TD\nA[开始] --> B[结束]\n```',
      );
    },
  );

  test(
    'unwrapStructuredText restores newline before fenced closing marker across structured segments',
    () {
      expect(
        ChatMessageContent.unwrapStructuredText(
          '{"content":[{"type":"text","text":"当前对话的会话记录文件在：\\n\\n```\\n"},{"type":"text","text":"/Users/mac/openclaw-shared/main/agents/main/sessions/d1ecae3f-eda5-48e7-8b27-483d7810b28f.jsonl"},{"type":"text","text":"```\\n\\n文件大小：474KB，格式为 JSONL（每行一个 JSON 对象）。"}]}',
        ),
        '当前对话的会话记录文件在：\n\n```\n/Users/mac/openclaw-shared/main/agents/main/sessions/d1ecae3f-eda5-48e7-8b27-483d7810b28f.jsonl\n```\n\n文件大小：474KB，格式为 JSONL（每行一个 JSON 对象）。',
      );
    },
  );

  test('unwrapStructuredText joins adjacent text blocks with a line break', () {
    expect(
      ChatMessageContent.unwrapStructuredText(
        '[{"type":"text","text":"段落一"},{"type":"text","text":"段落二"}]',
      ),
      '段落一\n段落二',
    );
  });

  test('unwrapStructuredText keeps single text block newlines intact', () {
    expect(
      ChatMessageContent.unwrapStructuredText(
        '[{"type":"text","text":"第一行\\n第二行\\n\\n第三段"}]',
      ),
      '第一行\n第二行\n\n第三段',
    );
  });

  test('tryUnwrapDispatchResult strips wrapper and keeps markdown body', () {
    const raw = '''
[dispatch-result]
**status**: completed
**summary**: done
[/dispatch-result]
''';
    expect(ChatMessageContent.isDispatchResultMessage(raw), isTrue);
    expect(
      ChatMessageContent.tryUnwrapDispatchResult(raw),
      '**status**: completed\n**summary**: done',
    );
    expect(
      ChatMessageContent.unwrapDispatchResult(raw),
      '**status**: completed\n**summary**: done',
    );
  });

  test('tryUnwrapDispatchResult rejects partial or surrounding text', () {
    expect(
      ChatMessageContent.tryUnwrapDispatchResult(
        '[dispatch-result]\nhello\n[/dispatch-result]\nextra',
      ),
      isNull,
    );
    expect(
      ChatMessageContent.tryUnwrapDispatchResult(
        'prefix [dispatch-result]\nx\n[/dispatch-result]',
      ),
      isNull,
    );
    expect(ChatMessageContent.unwrapDispatchResult('plain'), 'plain');
  });

  test(
    'tryUnwrapDispatchResult trims outer spaces and newlines before match',
    () {
      const raw =
          '  \n\t[dispatch-result]\n**status**: completed\n[/dispatch-result]\n  ';
      expect(ChatMessageContent.isDispatchResultMessage(raw), isTrue);
      expect(
        ChatMessageContent.tryUnwrapDispatchResult(raw),
        '**status**: completed',
      );
    },
  );
}
