import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_scroll_controller.dart';
import 'package:grix/modules/chat/widgets/chat_history_list.dart';

enum _EntryKind { wideEmbedded, ordinaryRoute }

class _EntryScenario {
  const _EntryScenario({
    required this.label,
    required this.platform,
    required this.kind,
    required this.windowSize,
    required this.listWidth,
  });

  final String label;
  final TargetPlatform platform;
  final _EntryKind kind;
  final Size windowSize;
  final double listWidth;
}

class _ProbeRow {
  const _ProbeRow(this.id);

  final String id;
}

class _HistoryProbe {
  _HistoryProbe(List<_ProbeRow> initialRows)
    : rows = ValueNotifier<List<_ProbeRow>>(initialRows),
      controller = ChatScrollController(keepScrollOffset: false);

  final ValueNotifier<List<_ProbeRow>> rows;
  final ChatScrollController controller;
  final Map<String, GlobalKey> rowKeys = {};
  int revision = 1;
  int generation = 1;
  int autoFillPages = 0;
  String positionReason = 'initial-tail-layout';

  GlobalKey _rowKey(String id) => rowKeys.putIfAbsent(id, GlobalKey.new);

  Widget buildList() {
    return ValueListenableBuilder<List<_ProbeRow>>(
      valueListenable: rows,
      builder: (_, currentRows, __) {
        return ChatHistoryList(
          controller: controller,
          itemKeys: [for (final row in currentRows) row.id],
          itemKey: (id) => rowKeys[id],
          delegate: SliverChildBuilderDelegate((_, index) {
            final row = currentRows[index];
            return SizedBox(
              key: _rowKey(row.id),
              height: 40,
              child: Text(row.id),
            );
          }, childCount: currentRows.length),
          padding: const EdgeInsets.symmetric(vertical: 12),
          cacheExtent: 1200,
          onRebase: () {},
          initiallyAtBottom: true,
        );
      },
    );
  }

  double? dy(WidgetTester tester, String id) {
    final key = rowKeys[id];
    if (key == null) return null;
    final finder = find.byKey(key);
    return finder.evaluate().isEmpty ? null : tester.getTopLeft(finder).dy;
  }

  Map<String, Object?> sample(
    WidgetTester tester, {
    required _EntryScenario scenario,
    required String frame,
    String? anchorId,
  }) {
    final currentRows = rows.value;
    final position = controller.position;
    final result = <String, Object?>{
      'frame': frame,
      'platform': scenario.platform.name,
      'entry': scenario.label,
      'revision': revision,
      'generation': generation,
      'autoFillPages': autoFillPages,
      'positionReason': positionReason,
      'firstId': currentRows.first.id,
      'firstDy': dy(tester, currentRows.first.id),
      'lastId': currentRows.last.id,
      'lastDy': dy(tester, currentRows.last.id),
      'anchorId': anchorId,
      'anchorDy': anchorId == null ? null : dy(tester, anchorId),
      'pixels': position.pixels,
      'min': position.minScrollExtent,
      'max': position.maxScrollExtent,
    };
    debugPrint('WIN-CHAT-01 ${jsonEncode(result)}');
    return result;
  }

  void dispose() {
    rows.dispose();
    controller.dispose();
  }
}

const _scenarios = [
  _EntryScenario(
    label: 'windows-wide-embedded-zero-transition',
    platform: TargetPlatform.windows,
    kind: _EntryKind.wideEmbedded,
    windowSize: Size(1200, 800),
    listWidth: 640,
  ),
  _EntryScenario(
    label: 'windows-ordinary-route-250ms',
    platform: TargetPlatform.windows,
    kind: _EntryKind.ordinaryRoute,
    windowSize: Size(900, 800),
    listWidth: 760,
  ),
  _EntryScenario(
    label: 'ios-ordinary-route-250ms',
    platform: TargetPlatform.iOS,
    kind: _EntryKind.ordinaryRoute,
    windowSize: Size(390, 844),
    listWidth: 390,
  ),
  _EntryScenario(
    label: 'android-ordinary-route-250ms',
    platform: TargetPlatform.android,
    kind: _EntryKind.ordinaryRoute,
    windowSize: Size(412, 915),
    listWidth: 412,
  ),
  _EntryScenario(
    label: 'macos-wide-embedded-zero-transition',
    platform: TargetPlatform.macOS,
    kind: _EntryKind.wideEmbedded,
    windowSize: Size(1200, 800),
    listWidth: 640,
  ),
];

Future<void> _mountEntry(
  WidgetTester tester,
  _EntryScenario scenario,
  _HistoryProbe probe,
) async {
  debugDefaultTargetPlatformOverride = scenario.platform;
  tester.view.physicalSize = scenario.windowSize;
  tester.view.devicePixelRatio = 1;

  Widget page() => Scaffold(
    body: Align(
      alignment: Alignment.topRight,
      child: SizedBox(
        width: scenario.listWidth,
        height: 300,
        child: probe.buildList(),
      ),
    ),
  );

  if (scenario.kind == _EntryKind.wideEmbedded) {
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: Row(
            children: [
              const Expanded(child: SizedBox.shrink()),
              SizedBox(
                width: scenario.listWidth,
                child: Navigator(
                  onGenerateRoute: (_) => PageRouteBuilder<void>(
                    transitionDuration: Duration.zero,
                    reverseTransitionDuration: Duration.zero,
                    pageBuilder: (_, __, ___) => page(),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
    return;
  }

  final navigatorKey = GlobalKey<NavigatorState>();
  await tester.pumpWidget(
    MaterialApp(
      navigatorKey: navigatorKey,
      home: const Scaffold(body: SizedBox.shrink()),
    ),
  );
  navigatorKey.currentState!.push<void>(
    PageRouteBuilder<void>(
      opaque: false,
      transitionDuration: const Duration(milliseconds: 250),
      reverseTransitionDuration: const Duration(milliseconds: 250),
      pageBuilder: (_, __, ___) => page(),
      transitionsBuilder: (_, animation, __, child) => SlideTransition(
        position: Tween<Offset>(
          begin: const Offset(1, 0),
          end: Offset.zero,
        ).animate(animation),
        child: child,
      ),
    ),
  );
  await tester.pump();
  await tester.pump();
}

String _firstMountedId(WidgetTester tester, _HistoryProbe probe) {
  return probe.rows.value
      .map((row) => row.id)
      .firstWhere((id) => probe.dy(tester, id) != null);
}

void _expectLatestRowOnBottom(WidgetTester tester, _HistoryProbe probe) {
  final listRect = tester.getRect(find.byType(ChatHistoryList));
  final lastTop = probe.dy(tester, probe.rows.value.last.id);
  expect(lastTop, isNotNull);
  expect(
    lastTop! + 40,
    closeTo(listRect.bottom - 12, 1.5),
    reason: 'latest row stays on the trailing edge',
  );
  final position = probe.controller.position;
  expect((position.maxScrollExtent - position.pixels).abs(), lessThan(1.5));
}

void _expectStableAnchor(List<Map<String, Object?>> frames) {
  final baseline = frames.first['anchorDy'];
  expect(baseline, isNotNull);
  for (final frame in frames.skip(1)) {
    expect(frame['anchorId'], frames.first['anchorId']);
    expect(frame['anchorDy'], isNotNull);
    expect(
      (frame['anchorDy']! as double) - (baseline! as double),
      closeTo(0, 1),
      reason: '${frame['entry']} ${frame['frame']} moved without new data',
    );
  }
}

Future<void> _unmountEntry(WidgetTester tester, _HistoryProbe probe) async {
  await tester.pumpWidget(const SizedBox.shrink());
  probe.dispose();
  debugDefaultTargetPlatformOverride = null;
  tester.view.resetPhysicalSize();
  tester.view.resetDevicePixelRatio();
}

void main() {
  for (final scenario in _scenarios) {
    for (final rowCount in [3, 8]) {
      final layout = rowCount == 3 ? 'short' : 'slightly-overflowing';
      testWidgets(
        '${scenario.label} keeps $layout history stable across visible frames',
        (tester) async {
          final probe = _HistoryProbe([
            for (var index = 1; index <= rowCount; index++)
              _ProbeRow('message-$index'),
          ]);
          addTearDown(() {
            debugDefaultTargetPlatformOverride = null;
            tester.view.resetPhysicalSize();
            tester.view.resetDevicePixelRatio();
          });
          await _mountEntry(tester, scenario, probe);
          final anchorId = _firstMountedId(tester, probe);
          final frames = <Map<String, Object?>>[
            probe.sample(
              tester,
              scenario: scenario,
              frame: 'first-visible',
              anchorId: anchorId,
            ),
          ];
          await tester.pump();
          frames.add(
            probe.sample(
              tester,
              scenario: scenario,
              frame: 'next-frame',
              anchorId: anchorId,
            ),
          );
          await tester.pump(const Duration(milliseconds: 80));
          frames.add(
            probe.sample(
              tester,
              scenario: scenario,
              frame: '80ms',
              anchorId: anchorId,
            ),
          );
          await tester.pump(const Duration(milliseconds: 170));
          frames.add(
            probe.sample(
              tester,
              scenario: scenario,
              frame: '250ms',
              anchorId: anchorId,
            ),
          );
          await tester.pump(const Duration(milliseconds: 750));
          frames.add(
            probe.sample(
              tester,
              scenario: scenario,
              frame: '1s',
              anchorId: anchorId,
            ),
          );

          _expectStableAnchor(frames);
          _expectLatestRowOnBottom(tester, probe);
          if (rowCount == 3) {
            expect(probe.controller.historyContentFitsViewport, isTrue);
            final listTop = tester.getRect(find.byType(ChatHistoryList)).top;
            expect(
              probe.dy(tester, probe.rows.value.first.id),
              greaterThan(listTop + 40),
              reason: 'unused space stays above a short history',
            );
          } else {
            expect(probe.controller.historyContentFitsViewport, isFalse);
            expect(frames.first['max'], greaterThan(1));
          }
          await _unmountEntry(tester, probe);
        },
      );
    }
  }

  testWidgets(
    'cached window, slow authoritative snapshot, and archive fill keep anchors',
    (tester) async {
      final scenario = _scenarios.first;
      final probe = _HistoryProbe(const [
        _ProbeRow('cached-1'),
        _ProbeRow('cached-2'),
        _ProbeRow('cached-3'),
      ]);
      addTearDown(() {
        debugDefaultTargetPlatformOverride = null;
        tester.view.resetPhysicalSize();
        tester.view.resetDevicePixelRatio();
      });
      await _mountEntry(tester, scenario, probe);
      const retainedAnchor = 'cached-3';
      final stableFrames = <Map<String, Object?>>[
        probe.sample(
          tester,
          scenario: scenario,
          frame: 'first-visible',
          anchorId: retainedAnchor,
        ),
      ];
      await tester.pump();
      stableFrames.add(
        probe.sample(
          tester,
          scenario: scenario,
          frame: 'next-frame',
          anchorId: retainedAnchor,
        ),
      );
      await tester.pump(const Duration(milliseconds: 80));
      stableFrames.add(
        probe.sample(
          tester,
          scenario: scenario,
          frame: '80ms-cache',
          anchorId: retainedAnchor,
        ),
      );

      probe.revision++;
      probe.positionReason = 'slow-db-authoritative-same-window';
      probe.rows.value = List<_ProbeRow>.of(probe.rows.value);
      await tester.pump();
      stableFrames.add(
        probe.sample(
          tester,
          scenario: scenario,
          frame: 'slow-db-published',
          anchorId: retainedAnchor,
        ),
      );
      await tester.pump(const Duration(milliseconds: 170));
      stableFrames.add(
        probe.sample(
          tester,
          scenario: scenario,
          frame: '250ms',
          anchorId: retainedAnchor,
        ),
      );
      _expectStableAnchor(stableFrames);

      probe.controller.prepareForWindowChange?.call(
        probe.rows.value.map((row) => row.id).toSet(),
      );
      probe.revision++;
      probe.autoFillPages++;
      probe.positionReason = 'archive-auto-fill-prepend';
      probe.rows.value = [
        const _ProbeRow('archive-1'),
        const _ProbeRow('archive-2'),
        const _ProbeRow('archive-3'),
        ...probe.rows.value,
      ];
      await tester.pump();
      final afterFill = probe.sample(
        tester,
        scenario: scenario,
        frame: 'archive-fill-frame',
        anchorId: retainedAnchor,
      );
      expect(
        (afterFill['anchorDy']! as double) -
            (stableFrames.last['anchorDy']! as double),
        closeTo(0, 1),
      );
      await tester.pump(const Duration(milliseconds: 750));
      final oneSecond = probe.sample(
        tester,
        scenario: scenario,
        frame: '1s',
        anchorId: retainedAnchor,
      );
      expect(
        (oneSecond['anchorDy']! as double) - (afterFill['anchorDy']! as double),
        closeTo(0, 1),
      );
      _expectLatestRowOnBottom(tester, probe);
      final olderTop = probe.dy(tester, 'archive-1');
      final retainedTop = probe.dy(tester, 'cached-1');
      expect(olderTop, isNotNull);
      expect(retainedTop, isNotNull);
      expect(olderTop!, lessThan(retainedTop!));
      final listTop = tester.getRect(find.byType(ChatHistoryList)).top;
      expect(
        olderTop,
        greaterThan(listTop),
        reason: 'older rows fill the space above the latest message',
      );
      await _unmountEntry(tester, probe);
    },
  );
}
