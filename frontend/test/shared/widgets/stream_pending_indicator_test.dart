import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/stream_pending_indicator.dart';

void main() {
  testWidgets('indicator advances the active dot every ~300ms', (
    WidgetTester tester,
  ) async {
    const color = Colors.green;
    // 与 widget 内相同的颜色计算（withValues 返回普通 Color，勿直接与
    // MaterialColor 常量比 ==）。
    final activeColor = color.withValues(alpha: 1);
    final inactiveColor = color.withValues(alpha: 0.28);

    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: StreamPendingIndicator(color: color)),
      ),
    );

    List<Color?> dotColors() => tester
        .widgetList<Container>(
          find.descendant(
            of: find.byType(StreamPendingIndicator),
            matching: find.byType(Container),
          ),
        )
        .map((c) => (c.decoration! as BoxDecoration).color)
        .toList();

    final before = dotColors();
    // 初始：恰好一个圆点高亮，其余为淡色。
    expect(before.where((c) => c == activeColor).length, 1);
    expect(before.where((c) => c == inactiveColor).length, 2);

    // 推进超过一个 tick（300ms）：高亮点移动到下一个索引，仍恰好一个。
    await tester.pump(const Duration(milliseconds: 350));
    final after = dotColors();
    expect(after.where((c) => c == activeColor).length, 1);
    expect(after, isNot(equals(before)));

    // 卸载以取消周期 Timer，避免测试框架报 pending timer。
    await tester.pumpWidget(const SizedBox());
  });

  testWidgets('indicator does not register a per-frame ticker', (
    WidgetTester tester,
  ) async {
    expect(tester.binding.transientCallbackCount, 0);
    await tester.pumpWidget(
      const MaterialApp(
        home: Scaffold(body: StreamPendingIndicator(color: Colors.green)),
      ),
    );
    // 旧实现用 AnimationController..repeat() 每帧驱动；改为 Timer 后不应再
    // 注册任何帧回调（60fps → ~3.3fps 的关键保证）。
    expect(tester.binding.transientCallbackCount, 0);

    await tester.pumpWidget(const SizedBox());
  });
}
