class StrictIntParser {
  const StrictIntParser._();

  static final RegExp _integerPattern = RegExp(r'^-?[0-9]+$');

  static int parse(dynamic value, {required String fieldName}) {
    final parsed = tryParse(value);
    if (parsed != null) return parsed;
    throw FormatException(
      '$fieldName expects integer number, got ${value.runtimeType}',
    );
  }

  static int? tryParse(dynamic value) {
    if (value == null) return null;
    if (value is int) return value;
    if (value is BigInt) return value.toInt();
    if (value is num) {
      final asInt = value.toInt();
      if (value == asInt) return asInt;
    }
    final raw = value is String ? value.trim() : value.toString().trim();
    if (raw.isEmpty || !_integerPattern.hasMatch(raw)) return null;
    return int.tryParse(raw);
  }
}
