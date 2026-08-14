import 'package:flutter_test/flutter_test.dart';

import 'package:grix/modules/chat/message_cards/services/chat_agent_card_action_encoder.dart';

void main() {
  group('buildOpenSessionUri', () {
    test('无卡片时只带 cwd', () {
      final uri = ChatAgentCardActionEncoder.buildOpenSessionUri('/a/b');
      final parsed = Uri.parse(uri);
      expect(parsed.scheme, 'grix');
      expect(parsed.host, 'open');
      expect(parsed.path, '/session');
      expect(parsed.queryParameters['cwd'], '/a/b');
      expect(parsed.queryParameters.containsKey('card_instance_id'), isFalse);
    });

    test('带 cardInstanceId 时附加 card_instance_id', () {
      final uri = ChatAgentCardActionEncoder.buildOpenSessionUri(
        '/a/b',
        cardInstanceId: 'card-1',
      );
      expect(Uri.parse(uri).queryParameters['card_instance_id'], 'card-1');
    });

    test('cwd 为空时抛 ArgumentError', () {
      expect(
        () => ChatAgentCardActionEncoder.buildOpenSessionUri('  '),
        throwsArgumentError,
      );
    });

    test('中文与空格路径可正确往返', () {
      const cwd = '/Users/me/我的 项目';
      final uri = ChatAgentCardActionEncoder.buildOpenSessionUri(cwd);
      expect(ChatAgentCardActionEncoder.tryParseOpenSessionCwd(uri), cwd);
    });
  });

  group('tryParseOpenSessionCwd', () {
    test('解析出 cwd', () {
      expect(
        ChatAgentCardActionEncoder.tryParseOpenSessionCwd(
          'grix://open/session?cwd=%2Fa%2Fb',
        ),
        '/a/b',
      );
    });

    test('带 card_instance_id 的老卡片 URI 也能解析', () {
      expect(
        ChatAgentCardActionEncoder.tryParseOpenSessionCwd(
          'grix://open/session?cwd=%2Fa&card_instance_id=c1',
        ),
        '/a',
      );
    });

    test('非绑定 URI 返回空串', () {
      expect(
        ChatAgentCardActionEncoder.tryParseOpenSessionCwd('hello world'),
        '',
      );
      expect(
        ChatAgentCardActionEncoder.tryParseOpenSessionCwd(
          'grix://card/agent_question_reply?d=%7B%7D',
        ),
        '',
      );
      expect(ChatAgentCardActionEncoder.tryParseOpenSessionCwd(''), '');
    });

    test('缺 cwd 参数返回空串', () {
      expect(
        ChatAgentCardActionEncoder.tryParseOpenSessionCwd(
          'grix://open/session',
        ),
        '',
      );
    });
  });
}
