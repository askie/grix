import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/modules/share_ingest/models/share_inbox_manifest.dart';
import 'package:grix/modules/share_ingest/share_ingest_page.dart';

class _FakeImService extends ImService {}

void main() {
  const channel = MethodChannel('grix/share_ingest');
  final deletedIds = <String>[];

  setUp(() {
    deletedIds.clear();
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async {
      if (call.method == 'deleteEntry') {
        deletedIds.add((call.arguments as Map)['id'] as String);
      }
      return null;
    });
    Get.put<ImService>(_FakeImService());
    Get.put<OssService>(OssService());
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
    Get.reset();
  });

  testWidgets(
    'dismissing the page with sendable content still clears the inbox entry',
    (tester) async {
      const manifest = ShareInboxManifest(
        id: 'entry-1',
        createdAt: 0,
        items: [ShareInboxItem(type: 'text', text: 'hello from wechat')],
      );

      await tester.pumpWidget(
        MaterialApp(
          home: Builder(
            builder: (context) => ElevatedButton(
              onPressed: () => Navigator.of(context).push<void>(
                MaterialPageRoute<void>(
                  builder: (_) => const ShareIngestPage(manifest: manifest),
                ),
              ),
              child: const Text('open'),
            ),
          ),
        ),
      );

      await tester.tap(find.text('open'));
      await tester.pumpAndSettle();
      expect(find.byType(ShareIngestPage), findsOneWidget);

      // Dismiss via the auto-added close/back button, not by sending.
      await tester.pageBack();
      await tester.pumpAndSettle();

      expect(deletedIds, contains('entry-1'));
    },
  );
}
