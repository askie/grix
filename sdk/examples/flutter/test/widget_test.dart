import 'package:flutter_test/flutter_test.dart';
import 'package:grix_widget_example/main.dart';

void main() {
  testWidgets('renders the SDK demo controls', (tester) async {
    await tester.pumpWidget(const GrixWidgetExampleApp());
    expect(find.text('Grix Widget Flutter Demo'), findsOneWidget);
    expect(find.text('present()'), findsOneWidget);
    expect(find.text('makeView()'), findsOneWidget);
  });
}
