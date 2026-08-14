import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/text_document/services/text_document_format_registry.dart';

void main() {
  test('recognizes Markdown and common source files case-insensitively', () {
    expect(TextDocumentFormatRegistry.isMarkdown('README.MD'), isTrue);
    expect(
      TextDocumentFormatRegistry.isSupported(fileName: 'server.go'),
      isTrue,
    );
    expect(
      TextDocumentFormatRegistry.isSupported(fileName: 'client.TS'),
      isTrue,
    );
  });

  test('uses text MIME as a fallback', () {
    expect(
      TextDocumentFormatRegistry.isSupported(
        fileName: 'LICENSE',
        mimeType: 'text/plain',
      ),
      isTrue,
    );
  });

  test('does not claim unrelated binary files', () {
    expect(
      TextDocumentFormatRegistry.isSupported(
        fileName: 'archive.zip',
        mimeType: 'application/zip',
      ),
      isFalse,
    );
  });
}
