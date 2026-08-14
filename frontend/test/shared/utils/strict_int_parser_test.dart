import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/utils/strict_int_parser.dart';

class _IntegerLikeValue {
  const _IntegerLikeValue(this.raw);

  final String raw;

  @override
  String toString() => raw;
}

void main() {
  group('StrictIntParser', () {
    test('accepts BigInt values', () {
      final value = StrictIntParser.parse(
        BigInt.parse('9223372036854775807'),
        fieldName: 'messages.max_msg_id',
      );

      expect(value, 9223372036854775807);
    });

    test('accepts integer-like objects from web sqlite adapters', () {
      final value = StrictIntParser.parse(
        const _IntegerLikeValue('9223372036854775807'),
        fieldName: 'messages.max_msg_id',
      );

      expect(value, 9223372036854775807);
    });

    test('rejects fractional numeric values', () {
      expect(
        () => StrictIntParser.parse(1.5, fieldName: 'messages.max_msg_id'),
        throwsFormatException,
      );
    });
  });
}
