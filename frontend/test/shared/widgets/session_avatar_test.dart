import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/models/session_avatar_member.dart';
import 'package:grix/shared/utils/user_image_cache_manager.dart';
import 'package:grix/shared/widgets/avatar_network_image.dart';
import 'package:grix/shared/widgets/session_avatar.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  const networkMember = SessionAvatarMember(
    memberId: 'network-member',
    memberType: 1,
    displayName: 'Network member',
    avatarUrl: 'https://example.invalid/avatar.png',
  );

  setUp(() {
    // Image.network remains behind flutter_test's mock HTTP client, so these
    // widget tests never reach the public network.
    UserImageCacheManager.setDisabledForTest(true);
  });

  tearDown(() {
    UserImageCacheManager.setDisabledForTest(false);
  });

  Widget buildApp({
    required ThemeData theme,
    required Key avatarKey,
    List<SessionAvatarMember> members = const <SessionAvatarMember>[
      networkMember,
    ],
    bool isGroup = true,
    String avatarUrl = '',
  }) {
    return MaterialApp(
      theme: theme,
      home: Scaffold(
        body: SessionAvatar(
          key: avatarKey,
          isGroup: isGroup,
          avatarTitle: 'Group',
          avatarColor: Colors.red,
          members: members,
          avatarUrl: avatarUrl,
        ),
      ),
    );
  }

  Finder networkCellBackdropFinder() => find.byWidgetPredicate(
    (widget) => widget is ColoredBox && widget.child is AvatarNetworkImage,
  );

  Color networkCellBackdrop(WidgetTester tester) {
    final finder = networkCellBackdropFinder();
    expect(finder, findsOneWidget);
    return tester.widget<ColoredBox>(finder).color;
  }

  testWidgets('group network avatar backdrop follows theme changes', (
    tester,
  ) async {
    final avatarKey = GlobalKey();
    final lightScheme = ColorScheme.fromSeed(
      seedColor: Colors.red,
      brightness: Brightness.light,
    );
    final darkScheme = ColorScheme.fromSeed(
      seedColor: Colors.red,
      brightness: Brightness.dark,
    );

    await tester.pumpWidget(
      buildApp(
        theme: ThemeData(colorScheme: lightScheme),
        avatarKey: avatarKey,
      ),
    );
    await tester.pumpAndSettle();
    final mountedState = avatarKey.currentState;
    expect(networkCellBackdrop(tester), Colors.white.withValues(alpha: 0.9));

    await tester.pumpWidget(
      buildApp(
        theme: ThemeData(colorScheme: darkScheme),
        avatarKey: avatarKey,
      ),
    );
    await tester.pumpAndSettle();
    expect(avatarKey.currentState, same(mountedState));
    expect(networkCellBackdrop(tester), darkScheme.surfaceContainerHighest);
  });

  testWidgets('group network avatar backdrop follows same-brightness palette', (
    tester,
  ) async {
    final avatarKey = GlobalKey();
    final firstScheme = ColorScheme.fromSeed(
      seedColor: Colors.red,
      brightness: Brightness.dark,
    );
    final secondScheme = ColorScheme.fromSeed(
      seedColor: Colors.blue,
      brightness: Brightness.dark,
    );
    expect(
      firstScheme.surfaceContainerHighest,
      isNot(secondScheme.surfaceContainerHighest),
    );

    await tester.pumpWidget(
      buildApp(
        theme: ThemeData(colorScheme: firstScheme),
        avatarKey: avatarKey,
      ),
    );
    await tester.pumpAndSettle();
    final mountedState = avatarKey.currentState;
    expect(networkCellBackdrop(tester), firstScheme.surfaceContainerHighest);

    await tester.pumpWidget(
      buildApp(
        theme: ThemeData(colorScheme: secondScheme),
        avatarKey: avatarKey,
      ),
    );
    await tester.pumpAndSettle();
    expect(avatarKey.currentState, same(mountedState));
    expect(networkCellBackdrop(tester), secondScheme.surfaceContainerHighest);
  });

  testWidgets('group member without avatar url keeps the text fallback', (
    tester,
  ) async {
    const fallbackColor = Color(0xFF123456);
    const member = SessionAvatarMember(
      memberId: 'fallback-member',
      memberType: 1,
      displayName: 'Fallback member',
      avatarUrl: '  ',
    );

    await tester.pumpWidget(
      MaterialApp(
        theme: ThemeData.dark(),
        home: const SessionAvatar(
          isGroup: true,
          avatarTitle: 'Group',
          avatarColor: Colors.red,
          memberFallbackColor: fallbackColor,
          members: <SessionAvatarMember>[member],
        ),
      ),
    );

    expect(find.text('F'), findsOneWidget);
    expect(find.byType(AvatarNetworkImage), findsNothing);
    final fallback = tester.widget<ColoredBox>(
      find.byWidgetPredicate(
        (widget) => widget is ColoredBox && widget.child is Center,
      ),
    );
    expect(fallback.color, fallbackColor.withValues(alpha: 0.9));
  });

  testWidgets('failed group network image shows the member fallback', (
    tester,
  ) async {
    await tester.pumpWidget(
      buildApp(theme: ThemeData.dark(), avatarKey: GlobalKey()),
    );
    await tester.pumpAndSettle();

    expect(find.text('N'), findsOneWidget);
    expect(networkCellBackdropFinder(), findsOneWidget);
  });

  testWidgets('single avatar without url keeps the existing fallback', (
    tester,
  ) async {
    final avatarKey = GlobalKey();
    await tester.pumpWidget(
      buildApp(theme: ThemeData.dark(), avatarKey: avatarKey, isGroup: false),
    );

    expect(find.text('G'), findsOneWidget);
    expect(find.byType(AvatarNetworkImage), findsOneWidget);
    expect(networkCellBackdropFinder(), findsNothing);
  });
}
