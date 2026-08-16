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
