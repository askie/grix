import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/bootstrap/bootstrap_loading_shell.dart';

void main() {
  final binding = TestWidgetsFlutterBinding.ensureInitialized();

  tearDown(() {
    binding.platformDispatcher.clearDefaultRouteNameTestValue();
    Get.reset();
  });

  testWidgets('accepts browser initial route without route exception', (
    tester,
  ) async {
    binding.platformDispatcher.defaultRouteNameTestValue = '/login';

    await tester.pumpWidget(const BootstrapLoadingShell(isLoading: true));
    await tester.pump();

    expect(tester.takeException(), isNull);
    expect(find.text('Grix'), findsOneWidget);
  });

  testWidgets('renders dark red Grix wordmark without logo image', (
    tester,
  ) async {
    await tester.pumpWidget(const BootstrapLoadingShell(isLoading: true));
    await tester.pump();

    final wordmark = tester.widget<Text>(find.text('Grix'));

    expect(find.byType(Image), findsNothing);
    expect(wordmark.style?.color, const Color(0xFFA51D2A));
  });
}
