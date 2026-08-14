import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:grix/shared/widgets/session_activity_indicator.dart';
import 'package:grix/shared/widgets/stream_pending_indicator.dart';

void main() {
  testWidgets('dark theme session activity indicator keeps readable text',
      (WidgetTester tester) async {
    await tester.pumpWidget(
      MaterialApp(
        theme: AppTheme.lightTheme,
        darkTheme: AppTheme.darkTheme,
        themeMode: ThemeMode.dark,
        home: const Scaffold(
          body: SessionActivityIndicator(
            label: '本地小虾米 is composing',
          ),
        ),
      ),
    );
    await tester.pump();

    final text = tester.widget<Text>(find.text('本地小虾米 is composing'));
    expect(
        text.style?.color, AppTheme.lightTextPrimary.withValues(alpha: 0.76));

    expect(find.byType(StreamPendingIndicator), findsOneWidget);
  });
}
