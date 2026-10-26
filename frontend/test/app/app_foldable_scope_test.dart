import 'dart:async';

import 'package:foldable/foldable.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/app_foldable_scope.dart';
import 'package:grix/shared/widgets/app_dialog_style.dart';

const _contentKey = Key('foldable_content');
const _inputKey = Key('foldable_input');
const _getDialogKey = Key('get_dialog');
const _materialDialogKey = Key('material_dialog');
const _bottomSheetKey = Key('bottom_sheet');
const _appDialogKey = Key('app_dialog');

FoldableData _duoData({
  required HingeStatus status,
  List<ReservedRegion> regions = const [],
  SizeClass horizontalSizeClass = SizeClass.regular,
}) {
  return FoldableData(
    capabilities: const FoldableCapabilities(
      supportLevel: FoldableSupportLevel.available,
      isFoldable: true,
      hingeApiPresent: true,
      regionApiPresent: true,
      angleUnitVerified: true,
      strategy: 'test',
    ),
    status: status,
    angleDegrees: switch (status) {
      HingeStatus.closed => 0,
      HingeStatus.partiallyOpen => 90,
      HingeStatus.fullyOpen => 180,
      HingeStatus.unknown => null,
    },
    regions: regions,
    displayFeatures: const [],
    horizontalSizeClass: horizontalSizeClass,
    verticalSizeClass: SizeClass.regular,
  );
}

Widget _contentHome() {
  return const Scaffold(
    body: Stack(
      fit: StackFit.expand,
      children: [
        SizedBox.expand(key: _contentKey),
        TextField(key: _inputKey),
      ],
    ),
  );
}

Widget _popupHome() {
  return Scaffold(
    body: Column(
      children: [
        ListTile(
          key: _getDialogKey,
          title: const Text('GetX dialog'),
          onTap: () =>
              Get.dialog<void>(const AlertDialog(title: Text('GetX dialog'))),
        ),
        Builder(
          builder: (context) => ListTile(
            key: _materialDialogKey,
            title: const Text('Material dialog'),
            onTap: () {
              showDialog<void>(
                context: context,
                builder: (_) =>
                    const AlertDialog(title: Text('Material dialog')),
              );
            },
          ),
        ),
        Builder(
          builder: (context) => ListTile(
            key: _appDialogKey,
            title: const Text('App dialog'),
            onTap: () {
              showAppConfirmDialog(
                context: context,
                title: 'App dialog',
                message: 'Confirm the Duo layout',
              );
            },
          ),
        ),
        Builder(
          builder: (context) => ListTile(
            key: _bottomSheetKey,
            title: const Text('Bottom sheet'),
            onTap: () {
              showModalBottomSheet<void>(
                context: context,
                builder: (_) => const SizedBox(
                  height: 240,
                  child: Center(child: Text('Bottom sheet')),
                ),
              );
            },
          ),
        ),
      ],
    ),
  );
}

Future<void> _pumpApp(
  WidgetTester tester,
  Stream<FoldableData> data,
  Size size, {
  Widget? home,
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1.0;
  addTearDown(tester.view.reset);
  await tester.pumpWidget(
    GetMaterialApp(
      builder: (context, child) => AppFoldableScope(
        debugData: data,
        child: child ?? const SizedBox.shrink(),
      ),
      home: home ?? _contentHome(),
    ),
  );
  await tester.pump();
}

Future<void> _emit(
  WidgetTester tester,
  StreamController<FoldableData> controller,
  FoldableData data,
) async {
  controller.add(data);
  await tester.pump();
  await tester.pump();
}

void _expectLeadingHalf(WidgetTester tester, Finder finder) {
  final rect = tester.getRect(finder);
  expect(rect.left, greaterThanOrEqualTo(0));
  expect(rect.right, lessThanOrEqualTo(455.5));
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late StreamController<FoldableData> controller;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    controller = StreamController<FoldableData>.broadcast();
  });

  tearDown(() async {
    await controller.close();
    Get.reset();
  });

  testWidgets('unsupported devices keep the existing full-window layout', (
    tester,
  ) async {
    await _pumpApp(tester, controller.stream, const Size(390, 844));
    await _emit(tester, controller, FoldableData.unsupported);

    expect(tester.getSize(find.byKey(_contentKey)), const Size(390, 844));
    expect(
      MediaQuery.of(tester.element(find.byKey(_contentKey))).displayFeatures,
      isEmpty,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('fully open and closed postures use the whole window', (
    tester,
  ) async {
    await _pumpApp(tester, controller.stream, const Size(951, 669));
    await _emit(tester, controller, _duoData(status: HingeStatus.fullyOpen));
    expect(tester.getSize(find.byKey(_contentKey)), const Size(951, 669));

    tester.view.physicalSize = const Size(466, 678);
    await tester.pump();
    await _emit(
      tester,
      controller,
      _duoData(
        status: HingeStatus.closed,
        horizontalSizeClass: SizeClass.compact,
      ),
    );
    expect(tester.getSize(find.byKey(_contentKey)), const Size(466, 678));
    expect(tester.takeException(), isNull);
  });

  testWidgets('vertical fold selects a subscreen and preserves input', (
    tester,
  ) async {
    const division = ReservedRegion(
      kind: ReservedRegionKind.division,
      bounds: Rect.fromLTWH(455.5, 0, 40, 669),
      isActive: true,
    );
    await _pumpApp(tester, controller.stream, const Size(951, 669));
    await _emit(tester, controller, _duoData(status: HingeStatus.fullyOpen));
    await tester.enterText(find.byKey(_inputKey), 'draft survives folding');

    await _emit(
      tester,
      controller,
      _duoData(status: HingeStatus.partiallyOpen, regions: [division]),
    );
    expect(tester.getSize(find.byKey(_contentKey)), const Size(455.5, 669));
    expect(find.text('draft survives folding'), findsOneWidget);

    await _emit(
      tester,
      controller,
      _duoData(status: HingeStatus.fullyOpen, regions: [division]),
    );
    expect(tester.getSize(find.byKey(_contentKey)), const Size(951, 669));
    expect(find.text('draft survives folding'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('horizontal fold selects the top subscreen', (tester) async {
    await _pumpApp(tester, controller.stream, const Size(669, 951));
    await _emit(
      tester,
      controller,
      _duoData(
        status: HingeStatus.partiallyOpen,
        regions: const [
          ReservedRegion(
            kind: ReservedRegionKind.division,
            bounds: Rect.fromLTWH(0, 455.5, 669, 40),
            isActive: true,
          ),
        ],
      ),
    );

    expect(tester.getSize(find.byKey(_contentKey)), const Size(669, 455.5));
    expect(tester.getTopLeft(find.byKey(_contentKey)), Offset.zero);
    expect(tester.takeException(), isNull);
  });

  testWidgets('GetX and Material popups stay inside the selected subscreen', (
    tester,
  ) async {
    await _pumpApp(
      tester,
      controller.stream,
      const Size(951, 669),
      home: _popupHome(),
    );
    await _emit(
      tester,
      controller,
      _duoData(
        status: HingeStatus.partiallyOpen,
        regions: const [
          ReservedRegion(
            kind: ReservedRegionKind.division,
            bounds: Rect.fromLTWH(455.5, 0, 40, 669),
            isActive: true,
          ),
        ],
      ),
    );

    await tester.tap(find.byKey(_getDialogKey));
    await tester.pumpAndSettle();
    _expectLeadingHalf(tester, find.byType(AlertDialog));
    Get.back();
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(_materialDialogKey));
    await tester.pumpAndSettle();
    _expectLeadingHalf(tester, find.byType(AlertDialog));
    Get.back();
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(_appDialogKey));
    await tester.pumpAndSettle();
    _expectLeadingHalf(tester, find.byType(AlertDialog));
    Get.back();
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(_bottomSheetKey));
    await tester.pumpAndSettle();
    _expectLeadingHalf(tester, find.byType(BottomSheet));
    expect(tester.takeException(), isNull);
  });
}
