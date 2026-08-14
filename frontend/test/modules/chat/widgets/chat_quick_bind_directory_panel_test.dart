import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/modules/chat/services/chat_recent_bind_directory_store.dart';
import 'package:grix/modules/chat/widgets/chat_quick_bind_directory_panel.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Widget wrap(Widget child) {
    return GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      home: Scaffold(body: Center(child: child)),
    );
  }

  List<RecentBindDirectoryEntry> sampleEntries() => const [
        RecentBindDirectoryEntry(
          path: '/Users/me/projects/aibot',
          agentId: 'agent1',
          hostname: 'mac1',
          updatedAtMs: 2,
        ),
        RecentBindDirectoryEntry(
          path: '/Users/me/projects/demo',
          agentId: 'agent1',
          hostname: 'mac1',
          updatedAtMs: 1,
        ),
      ];

  testWidgets('渲染历史目录列表与选择目录按钮', (tester) async {
    await tester.pumpWidget(
      wrap(
        ChatQuickBindDirectoryPanel(
          entriesLoader: () async => sampleEntries(),
          onBindDirectory: (_) async => true,
          onPickDirectory: () async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('chat_quick_bind_directory_panel')),
        findsOneWidget);
    expect(find.text('aibot'), findsOneWidget);
    expect(find.text('demo'), findsOneWidget);
    expect(find.text('/Users/me/projects/aibot'), findsOneWidget);
    expect(
      find.byKey(const Key('chat_quick_bind_pick_button')),
      findsOneWidget,
    );
  });

  testWidgets('无历史记录时仍显示选择目录按钮', (tester) async {
    await tester.pumpWidget(
      wrap(
        ChatQuickBindDirectoryPanel(
          entriesLoader: () async => const [],
          onBindDirectory: (_) async => true,
          onPickDirectory: () async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(
      find.byKey(const Key('chat_quick_bind_pick_button')),
      findsOneWidget,
    );
    expect(find.byType(ListView), findsNothing);
  });

  testWidgets('点击历史目录触发绑定回调', (tester) async {
    final bound = <String>[];
    await tester.pumpWidget(
      wrap(
        ChatQuickBindDirectoryPanel(
          entriesLoader: () async => sampleEntries(),
          onBindDirectory: (path) async {
            bound.add(path);
            return true;
          },
          onPickDirectory: () async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_quick_bind_entry_/Users/me/projects/demo')),
    );
    await tester.pumpAndSettle();

    expect(bound, ['/Users/me/projects/demo']);
  });

  testWidgets('选择目录返回路径后触发绑定；取消不触发', (tester) async {
    final bound = <String>[];
    String? nextPick = '/picked/dir';
    await tester.pumpWidget(
      wrap(
        ChatQuickBindDirectoryPanel(
          entriesLoader: () async => const [],
          onBindDirectory: (path) async {
            bound.add(path);
            return true;
          },
          onPickDirectory: () async => nextPick,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('chat_quick_bind_pick_button')));
    await tester.pumpAndSettle();
    expect(bound, ['/picked/dir']);

    nextPick = null;
    await tester.tap(find.byKey(const Key('chat_quick_bind_pick_button')));
    await tester.pumpAndSettle();
    expect(bound, ['/picked/dir']);
  });

  testWidgets('设置揭示延迟后延迟内不显示，到点才显示', (tester) async {
    await tester.pumpWidget(
      wrap(
        ChatQuickBindDirectoryPanel(
          entriesLoader: () async => sampleEntries(),
          onBindDirectory: (_) async => true,
          onPickDirectory: () async => null,
          revealDelay: const Duration(milliseconds: 500),
        ),
      ),
    );
    // 缓存已加载但揭示延迟未到，组件不应出现。
    await tester.pump(const Duration(milliseconds: 100));
    expect(
      find.byKey(const Key('chat_quick_bind_directory_panel')),
      findsNothing,
    );

    // 延迟到点后组件显示。
    await tester.pump(const Duration(milliseconds: 500));
    expect(
      find.byKey(const Key('chat_quick_bind_directory_panel')),
      findsOneWidget,
    );
  });

  testWidgets('揭示延迟内被移除不报错（消息到达使空态消失）', (tester) async {
    await tester.pumpWidget(
      wrap(
        ChatQuickBindDirectoryPanel(
          entriesLoader: () async => sampleEntries(),
          onBindDirectory: (_) async => true,
          onPickDirectory: () async => null,
          revealDelay: const Duration(milliseconds: 500),
        ),
      ),
    );
    await tester.pump(const Duration(milliseconds: 100));
    // 延迟未到就整体移除，模拟消息加载后空态消失。
    await tester.pumpWidget(wrap(const SizedBox.shrink()));
    await tester.pump(const Duration(milliseconds: 600));
    expect(tester.takeException(), isNull);
  });

  testWidgets('绑定进行中禁止重复提交', (tester) async {
    var bindCalls = 0;
    await tester.pumpWidget(
      wrap(
        ChatQuickBindDirectoryPanel(
          entriesLoader: () async => sampleEntries(),
          onBindDirectory: (_) async {
            bindCalls++;
            await Future<void>.delayed(const Duration(milliseconds: 300));
            return true;
          },
          onPickDirectory: () async => null,
        ),
      ),
    );
    await tester.pumpAndSettle();

    await tester.tap(
      find.byKey(const Key('chat_quick_bind_entry_/Users/me/projects/aibot')),
    );
    await tester.pump(const Duration(milliseconds: 50));
    // 提交中再点另一条不应触发第二次绑定。
    await tester.tap(
      find.byKey(const Key('chat_quick_bind_entry_/Users/me/projects/demo')),
      warnIfMissed: false,
    );
    await tester.pumpAndSettle();

    expect(bindCalls, 1);
  });
}
