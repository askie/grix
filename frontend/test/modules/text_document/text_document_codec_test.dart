import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/text_document/models/text_document_content.dart';
import 'package:grix/modules/text_document/services/text_document_codec.dart';

void main() {
  test('UTF-8 BOM and CRLF survive decode and encode', () {
    final input = Uint8List.fromList([
      0xEF,
      0xBB,
      0xBF,
      ...utf8.encode('hello\r\n世界\r\n'),
    ]);

    final decoded = TextDocumentCodec.decode(input);

    expect(decoded.encoding, TextDocumentEncoding.utf8Bom);
    expect(decoded.lineEnding, '\r\n');
    expect(decoded.text, 'hello\r\n世界\r\n');
    expect(
      TextDocumentCodec.encode(decoded.text, decoded.encoding),
      orderedEquals(input),
    );
  });

  test('UTF-16 little endian survives decode and encode', () {
    final input = TextDocumentCodec.encode(
      'package main\n',
      TextDocumentEncoding.utf16LittleEndian,
    );

    final decoded = TextDocumentCodec.decode(input);

    expect(decoded.encoding, TextDocumentEncoding.utf16LittleEndian);
    expect(decoded.text, 'package main\n');
    expect(
      TextDocumentCodec.encode(decoded.text, decoded.encoding),
      orderedEquals(input),
    );
  });

  test('binary data is rejected', () {
    expect(
      () => TextDocumentCodec.decode(Uint8List.fromList([1, 2, 0, 4])),
      throwsFormatException,
    );
  });

  test('oversized input is rejected before decoding', () {
    expect(
      () => TextDocumentCodec.decode(
        Uint8List(TextDocumentCodec.maxPreviewBytes + 1),
      ),
      throwsFormatException,
    );
  });
}
