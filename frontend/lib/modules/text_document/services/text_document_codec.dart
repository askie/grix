import 'dart:convert';
import 'dart:typed_data';

import '../models/text_document_content.dart';

class TextDocumentCodec {
  const TextDocumentCodec._();

  static const int maxEditableBytes = 2 * 1024 * 1024;
  static const int maxPreviewBytes = 10 * 1024 * 1024;

  static TextDocumentContent decode(Uint8List bytes) {
    if (bytes.length > maxPreviewBytes) {
      throw const FormatException('text_document_too_large');
    }
    late final String text;
    late final TextDocumentEncoding encoding;
    if (_startsWith(bytes, const [0xEF, 0xBB, 0xBF])) {
      text = utf8.decode(bytes.sublist(3), allowMalformed: false);
      encoding = TextDocumentEncoding.utf8Bom;
    } else if (_startsWith(bytes, const [0xFF, 0xFE])) {
      text = _decodeUtf16(bytes.sublist(2), littleEndian: true);
      encoding = TextDocumentEncoding.utf16LittleEndian;
    } else if (_startsWith(bytes, const [0xFE, 0xFF])) {
      text = _decodeUtf16(bytes.sublist(2), littleEndian: false);
      encoding = TextDocumentEncoding.utf16BigEndian;
    } else {
      if (bytes.contains(0)) {
        throw const FormatException('text_document_binary');
      }
      text = utf8.decode(bytes, allowMalformed: false);
      encoding = TextDocumentEncoding.utf8;
    }
    if (_looksBinary(text)) {
      throw const FormatException('text_document_binary');
    }
    return TextDocumentContent(
      text: text,
      encoding: encoding,
      lineEnding: text.contains('\r\n') ? '\r\n' : '\n',
      originalBytes: Uint8List.fromList(bytes),
    );
  }

  static Uint8List encode(String text, TextDocumentEncoding encoding) {
    return switch (encoding) {
      TextDocumentEncoding.utf8 => Uint8List.fromList(utf8.encode(text)),
      TextDocumentEncoding.utf8Bom => Uint8List.fromList([
        0xEF,
        0xBB,
        0xBF,
        ...utf8.encode(text),
      ]),
      TextDocumentEncoding.utf16LittleEndian => Uint8List.fromList([
        0xFF,
        0xFE,
        ..._encodeUtf16(text, littleEndian: true),
      ]),
      TextDocumentEncoding.utf16BigEndian => Uint8List.fromList([
        0xFE,
        0xFF,
        ..._encodeUtf16(text, littleEndian: false),
      ]),
    };
  }

  static bool _startsWith(Uint8List bytes, List<int> prefix) {
    if (bytes.length < prefix.length) return false;
    for (var i = 0; i < prefix.length; i++) {
      if (bytes[i] != prefix[i]) return false;
    }
    return true;
  }

  static String _decodeUtf16(List<int> bytes, {required bool littleEndian}) {
    if (bytes.length.isOdd) {
      throw const FormatException('text_document_invalid_utf16');
    }
    final units = <int>[];
    for (var i = 0; i < bytes.length; i += 2) {
      units.add(
        littleEndian
            ? bytes[i] | (bytes[i + 1] << 8)
            : (bytes[i] << 8) | bytes[i + 1],
      );
    }
    return String.fromCharCodes(units);
  }

  static List<int> _encodeUtf16(String text, {required bool littleEndian}) {
    final result = <int>[];
    for (final unit in text.codeUnits) {
      if (littleEndian) {
        result
          ..add(unit & 0xFF)
          ..add(unit >> 8);
      } else {
        result
          ..add(unit >> 8)
          ..add(unit & 0xFF);
      }
    }
    return result;
  }

  static bool _looksBinary(String text) {
    if (text.isEmpty) return false;
    var controls = 0;
    for (final unit in text.codeUnits.take(4096)) {
      if (unit == 0) return true;
      if (unit < 0x20 && unit != 0x09 && unit != 0x0A && unit != 0x0D) {
        controls++;
      }
    }
    return controls > 8 && controls / text.length.clamp(1, 4096) > 0.02;
  }
}
