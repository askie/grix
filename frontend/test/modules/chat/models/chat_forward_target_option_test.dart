import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/models/chat_forward_target_option.dart';

void main() {
  group('ChatForwardTargetOption subtitle (lazy preview)', () {
    test('derives cleaned subtitle lazily from previewSource', () {
      final option = ChatForwardTargetOption(
        sessionId: 's1',
        avatarColorSeed: 's1',
        title: 'Alpha',
        isGroup: false,
        activityAt: 1,
        previewSource: '![img](http://x/a.png) **hello** world',
      );

      // 原文原样保留，未在构建列表时被预清洗（懒计算的前提）。
      expect(option.previewSource, '![img](http://x/a.png) **hello** world');

      // 访问时才清洗：图片占位/加粗标记被去除，等价于旧的 eager summarize 结果。
      final subtitle = option.subtitle;
      expect(subtitle.contains('http://x/a.png'), isFalse);
      expect(subtitle.contains('**'), isFalse);
      expect(subtitle.contains('hello'), isTrue);

      // 二次访问命中缓存，返回同一结果。
      expect(identical(option.subtitle, option.subtitle), isTrue);
    });

    test('explicit subtitle takes precedence over previewSource', () {
      final option = ChatForwardTargetOption(
        sessionId: 's2',
        avatarColorSeed: 's2',
        title: 'Beta',
        isGroup: false,
        activityAt: 1,
        subtitle: 'explicit',
        previewSource: 'should be ignored',
      );
      expect(option.subtitle, 'explicit');
    });

    test('empty previewSource yields empty subtitle', () {
      final option = ChatForwardTargetOption(
        sessionId: 's3',
        avatarColorSeed: 's3',
        title: 'Gamma',
        isGroup: false,
        activityAt: 1,
      );
      expect(option.subtitle, isEmpty);
    });
  });
}
