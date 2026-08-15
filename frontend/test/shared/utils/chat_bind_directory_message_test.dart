import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/chat_bind_directory_message.dart';
import 'package:grix/shared/utils/chat_message_preview.dart';

void main() {
  const bindUri = 'grix://open/session?cwd=%2Fworkspace%2Fgrix';

  group('tryParseCwd', () {
    test('parses cwd from bind directive uri', () {
      expect(
        ChatBindDirectoryMessage.tryParseCwd(bindUri),
        '/workspace/grix',
      );
    });

    test('parses uri with card_instance_id', () {
      expect(
        ChatBindDirectoryMessage.tryParseCwd(
          'grix://open/session?cwd=%2Ftmp%2Fdemo&card_instance_id=abc',
        ),
        '/tmp/demo',
      );
    });

    test('returns empty for non-bind content', () {
      expect(ChatBindDirectoryMessage.tryParseCwd('hello world'), '');
      expect(
        ChatBindDirectoryMessage.tryParseCwd('https://example.com?cwd=%2Ftmp'),
        '',
      );
      expect(
        ChatBindDirectoryMessage.tryParseCwd('grix://card/progress?d=%7B%7D'),
        '',
      );
      expect(ChatBindDirectoryMessage.tryParseCwd('grix://open/session'), '');
      expect(
        ChatBindDirectoryMessage.tryParseCwd('看看这个 $bindUri 链接'),
        '',
      );
    });
  });

  group('friendly text', () {
    test('friendlyText keeps full path', () {
      expect(
        ChatBindDirectoryMessage.friendlyText(bindUri),
        '绑定目录 /workspace/grix',
      );
    });

    test('friendlyShortText keeps only directory basename', () {
      expect(
        ChatBindDirectoryMessage.friendlyShortText(bindUri),
        '绑定目录 grix',
      );
    });

    test('friendlyText returns empty for non-bind content', () {
      expect(ChatBindDirectoryMessage.friendlyText('普通消息'), '');
    });
  });

  group('ChatMessagePreview integration', () {
    test('summarize renders bind directive as short friendly text', () {
      expect(ChatMessagePreview.summarize(bindUri), '绑定目录 grix');
    });

    test('summarizeTitle renders bind directive as short friendly text', () {
      expect(ChatMessagePreview.summarizeTitle(bindUri), '绑定目录 grix');
    });
  });
}
