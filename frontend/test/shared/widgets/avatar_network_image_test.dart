import 'package:cached_network_image/cached_network_image.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/avatar_network_image.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  Widget buildTestApp({required Widget child}) {
    return MaterialApp(
      home: Scaffold(body: Center(child: child)),
    );
  }

  testWidgets('returns fallback when avatar url is empty', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildTestApp(
        child: const SizedBox(
          width: 48,
          height: 48,
          child: AvatarNetworkImage(
            avatarUrl: '  ',
            fallback: Text('fallback'),
          ),
        ),
      ),
    );

    expect(find.text('fallback'), findsOneWidget);
    expect(find.byType(CachedNetworkImage), findsNothing);
  });

  testWidgets('uses cached network image with stable avatar settings', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildTestApp(
        child: const SizedBox(
          width: 48,
          height: 48,
          child: AvatarNetworkImage(
            avatarUrl: 'https://example.com/avatar.png',
            fallback: Text('fallback'),
          ),
        ),
      ),
    );

    final cachedImage = tester.widget<CachedNetworkImage>(
      find.byType(CachedNetworkImage),
    );

    expect(cachedImage.imageUrl, 'https://example.com/avatar.png');
    expect(cachedImage.cacheKey, 'https://example.com/avatar.png');
    expect(cachedImage.useOldImageOnUrlChange, isTrue);
    expect(cachedImage.fadeInDuration, Duration.zero);
    expect(cachedImage.fadeOutDuration, Duration.zero);
    expect(cachedImage.placeholderFadeInDuration, Duration.zero);
  });

  testWidgets('uses normalized cache key for signed avatar urls', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      buildTestApp(
        child: const SizedBox(
          width: 48,
          height: 48,
          child: AvatarNetworkImage(
            avatarUrl:
                ' https://cdn.example.com/u/avatar.png?Expires=10&Signature=a&v=2#frag ',
            fallback: Text('fallback'),
          ),
        ),
      ),
    );

    final cachedImage = tester.widget<CachedNetworkImage>(
      find.byType(CachedNetworkImage),
    );

    expect(
      cachedImage.imageUrl,
      'https://cdn.example.com/u/avatar.png?Expires=10&Signature=a&v=2#frag',
    );
    expect(cachedImage.cacheKey, 'https://cdn.example.com/u/avatar.png?v=2');
  });
}
