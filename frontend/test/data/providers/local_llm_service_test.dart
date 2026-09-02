import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_llm_service.dart';

void main() {
  test(
    'streaming utf8 line decoder preserves emoji split across byte chunks',
    () {
      final decoder = StreamingUtf8LineDecoder();
      final prefix = utf8.encode('data: {"choices":[{"delta":{"content":"');
      final emojiBytes = utf8.encode('🍊');
      final suffix = utf8.encode('"}}]}\n');

      final firstChunk = <int>[...prefix, emojiBytes[0], emojiBytes[1]];
      final secondChunk = <int>[emojiBytes[2], emojiBytes[3], ...suffix];

      expect(decoder.add(firstChunk), isEmpty);

      final lines = decoder.add(secondChunk);
      expect(lines, hasLength(1));
      expect(lines.single, contains('🍊'));
      expect(lines.single, isNot(contains('\uFFFD')));
      expect(decoder.close(), isEmpty);
    },
  );

  test('streaming utf8 line decoder flushes trailing line on close', () {
    final decoder = StreamingUtf8LineDecoder();

    expect(decoder.add(utf8.encode('data: {"x":1}')), isEmpty);

    final trailing = decoder.close();
    expect(trailing, ['data: {"x":1}']);
  });
}
