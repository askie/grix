import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/ai/utils/agent_introduction_mention.dart';

void main() {
  group('buildIntroductionMentionInsertText', () {
    test('wraps display name with @ and trailing space at end', () {
      expect(
        buildIntroductionMentionInsertText(
          prefix: 'hello',
          suffix: '',
          displayName: 'Carol',
        ),
        ' @Carol ',
      );
    });

    test('avoids double spaces when surrounding boundaries exist', () {
      expect(
        buildIntroductionMentionInsertText(
          prefix: 'hello ',
          suffix: ' world',
          displayName: 'Carol',
        ),
        '@Carol',
      );
    });
  });

  group('normalizeIntroductionMentions', () {
    test('replaces display names with ids', () {
      final normalized =
          normalizeIntroductionMentions('请联系 @Carol 和 @Beta Bot 处理', const [
            AgentIntroductionMention(id: '1001', displayName: 'Carol'),
            AgentIntroductionMention(id: '2002', displayName: 'Beta Bot'),
          ]);
      expect(normalized, '请联系 @1001 和 @2002 处理');
    });

    test('prefers longer display names first', () {
      final normalized = normalizeIntroductionMentions('@Carol Smith', const [
        AgentIntroductionMention(id: '1', displayName: 'Carol'),
        AgentIntroductionMention(id: '2', displayName: 'Carol Smith'),
      ]);
      expect(normalized, '@2');
    });
  });

  group('hydrateIntroductionMentions', () {
    test('restores nicknames from known ids', () {
      final hydrated = hydrateIntroductionMentions('请联系 @1001', const {
        '1001': 'Carol',
      });
      expect(hydrated.text, '请联系 @Carol');
      expect(hydrated.mentions, hasLength(1));
      expect(hydrated.mentions.single.id, '1001');
      expect(hydrated.mentions.single.displayName, 'Carol');
    });
  });
}
