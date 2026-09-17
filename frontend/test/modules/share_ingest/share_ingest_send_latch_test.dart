import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/models/session_model.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/modules/share_ingest/models/share_inbox_manifest.dart';
import 'package:grix/modules/share_ingest/share_ingest_page.dart';

class _RecordingImService extends ImService {
  final List<String> sentContents = <String>[];
  final Completer<void> pendingSend = Completer<void>();

  @override
  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) {
    sentContents.add(content);
    return pendingSend.future;
  }
}

void main() {
  const channel = MethodChannel('grix/share_ingest');

  setUp(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, (call) async => null);
    Get.put<OssService>(OssService());
  });

  tearDown(() {
    TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger
        .setMockMethodCallHandler(channel, null);
    Get.reset();
  });

  testWidgets('tapping a target session twice sends the share only once', (
    tester,
  ) async {
    final im = _RecordingImService();
    im.sessions.add(
      SessionModel(
        sessionId: 'session-1',
        title: 'Agent One',
        peerId: 'agent-1',
        peerType: 2,
        updatedAt: 1,
        lastMessageTime: 1,
      ),
    );
    Get.put<ImService>(im);

    const manifest = ShareInboxManifest(
      id: 'entry-1',
      createdAt: 0,
      items: [ShareInboxItem(type: 'text', text: 'hello from wechat')],
    );

    await tester.pumpWidget(
      const MaterialApp(home: ShareIngestPage(manifest: manifest)),
    );
    await tester.pump();

    final target = find.text('Agent One');
    await tester.ensureVisible(target);
    await tester.pump();

    await tester.tap(target);
    await tester.tap(target);
    await tester.pump();

    expect(im.sentContents, hasLength(1));
    expect(im.sentContents.single, contains('hello from wechat'));
  });
}
