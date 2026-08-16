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
    expect(find.byType(GetMaterialApp), findsNothing);
    expect(find.byType(MaterialApp), findsOneWidget);
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

  testWidgets('hands off cleanly to the GetX application root', (tester) async {
    final key = GlobalKey<_BootstrapHandoffHostState>();

    await tester.pumpWidget(_BootstrapHandoffHost(key: key));
    expect(find.byType(GetMaterialApp), findsNothing);
    expect(find.text('Grix'), findsOneWidget);

    key.currentState!.completeBootstrap();
    await tester.pump();

    expect(tester.takeException(), isNull);
    expect(find.byType(GetMaterialApp), findsOneWidget);
    expect(find.text('Ready'), findsOneWidget);
  });
}

class _BootstrapHandoffHost extends StatefulWidget {
  const _BootstrapHandoffHost({super.key});

  @override
  State<_BootstrapHandoffHost> createState() => _BootstrapHandoffHostState();
}

class _BootstrapHandoffHostState extends State<_BootstrapHandoffHost> {
  bool _loading = true;

  void completeBootstrap() => setState(() => _loading = false);

  @override
  Widget build(BuildContext context) {
    if (_loading) {
      return const BootstrapLoadingShell(isLoading: true);
    }
    return const GetMaterialApp(home: Scaffold(body: Text('Ready')));
  }
}
