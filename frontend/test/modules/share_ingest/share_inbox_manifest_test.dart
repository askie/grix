import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/share_ingest/models/share_inbox_manifest.dart';

void main() {
  test('parses inbox manifest json', () {
    const raw = '''
{
  "id": "abc",
  "created_at": 1700000000000,
  "items": [
    {"type": "text", "text": "hello"},
    {"type": "file", "path": "item_0_a.png", "file_name": "a.png", "mime": "image/png", "size": 12}
  ],
  "source": "wechat"
}
''';
    final manifest = ShareInboxManifest.tryParseJson(raw);
    expect(manifest, isNotNull);
    expect(manifest!.id, 'abc');
    expect(manifest.items, hasLength(2));
    expect(manifest.items.first.text, 'hello');
    expect(manifest.items.last.fileName, 'a.png');
  });
}
