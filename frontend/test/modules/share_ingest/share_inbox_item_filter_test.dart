import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/share_ingest/models/share_inbox_manifest.dart';
import 'package:grix/modules/share_ingest/services/share_inbox_item_filter.dart';

void main() {
  test('file:// plain text items are excluded from shared body', () {
    const items = <ShareInboxItem>[
      ShareInboxItem(type: 'text', text: 'file:///var/mobile/Containers/foo/doc.pdf'),
      ShareInboxItem(type: 'text', text: 'real note'),
      ShareInboxItem(type: 'url', text: 'https://example.com'),
    ];
    expect(ShareInboxItemFilter.isFileUrlTextItem(items.first), isTrue);
    expect(
      ShareInboxItemFilter.collectSharedTextBody(items),
      'real note\nhttps://example.com',
    );
  });
}
