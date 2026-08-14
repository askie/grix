import 'dart:typed_data';

enum TextDocumentEncoding { utf8, utf8Bom, utf16LittleEndian, utf16BigEndian }

class TextDocumentContent {
  const TextDocumentContent({
    required this.text,
    required this.encoding,
    required this.lineEnding,
    required this.originalBytes,
  });

  final String text;
  final TextDocumentEncoding encoding;
  final String lineEnding;
  final Uint8List originalBytes;

  bool get hasBom => encoding != TextDocumentEncoding.utf8;
}
