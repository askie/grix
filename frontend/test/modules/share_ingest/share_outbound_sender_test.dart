import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/share_ingest/services/share_outbound_sender.dart';

void main() {
  test('long text attachment bytes are UTF-8 and round-trip with emoji', () {
    final text = '${'中' * 50000}🎉${'文' * 50001}';
    expect(text.runes.length, greaterThan(ShareOutboundSender.maxTextMessageRunes));

    final bytes = ShareOutboundSender.encodeTextAsUtf8AttachmentBytes(text);
    expect(utf8.decode(bytes), text);
  });
}
