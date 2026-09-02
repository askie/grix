import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/feature_gate.dart';
import 'package:grix/data/providers/feature_flag_service.dart';
import 'package:get/get.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('FeatureGate', () {
    late FeatureFlagService service;

    setUp(() {
      service = FeatureFlagService();
      Get.put<FeatureFlagService>(service);
    });

    tearDown(() {
      Get.reset();
    });

    testWidgets('shows child when feature is enabled', (tester) async {
      service.features.value = ['voice_call'];

      await tester.pumpWidget(
        const MaterialApp(
          home: FeatureGate(feature: 'voice_call', child: Text('Visible')),
        ),
      );

      expect(find.text('Visible'), findsOneWidget);
    });

    testWidgets('hides child when feature is disabled', (tester) async {
      service.features.value = [];

      await tester.pumpWidget(
        const MaterialApp(
          home: FeatureGate(feature: 'voice_call', child: Text('Hidden')),
        ),
      );

      expect(find.text('Hidden'), findsNothing);
    });

    testWidgets(
      'shows fallback when feature is disabled and fallback provided',
      (tester) async {
        service.features.value = [];

        await tester.pumpWidget(
          const MaterialApp(
            home: FeatureGate(
              feature: 'voice_call',
              fallback: Text('Fallback'),
              child: Text('Hidden'),
            ),
          ),
        );

        expect(find.text('Hidden'), findsNothing);
        expect(find.text('Fallback'), findsOneWidget);
      },
    );
  });
}

// Minimal MaterialApp wrapper for testing
class MaterialApp extends StatelessWidget {
  final Widget home;
  const MaterialApp({super.key, required this.home});

  @override
  Widget build(BuildContext context) {
    return Directionality(textDirection: TextDirection.ltr, child: home);
  }
}
