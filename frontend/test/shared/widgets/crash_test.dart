import 'package:flutter_test/flutter_test.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();
  testWidgets('Reproduce get language error', (tester) async {
    const text = '    | 表格 | 空格 |\n    | --- | --- |\n    | 1 | 2 |';
    final tableInCodeBlockPattern = RegExp(
      r'^ *\|?([ \t]*:?\-+:?[ \t]*\|[ \t]*)+([ \t]|[ \t]*:?\-+:?[ \t]*)?$',
      multiLine: true,
    );
    expect(tableInCodeBlockPattern.hasMatch(text), isTrue);
  });
}
