import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/modules/share_ingest/models/share_inbox_manifest.dart';
import 'package:grix/modules/share_ingest/services/share_outbound_sender.dart';

class _RecordingImService extends ImService {
  int sendCount = 0;
  String? lastContent;
  String? lastSessionId;
  Map<String, dynamic>? lastExtra;

  @override
  Future<void> sendMessage(
    String content,
    String sessionId, {
    Map<String, dynamic>? extra,
    String? quotedMessageId,
    List<String>? visibleTo,
    bool updateCurrentSessionUi = true,
  }) async {
    sendCount++;
    lastContent = content;
    lastSessionId = sessionId;
    lastExtra = extra == null ? null : Map<String, dynamic>.from(extra);
  }
}

class _FakeOssService extends OssService {
  _FakeOssService({this.failPresignOnCall});

  int presignCalls = 0;
  final int? failPresignOnCall;

  @override
  Future<Map<String, String>?> getPresignedUrl(
    String filename,
    String contentType,
  ) async {
    presignCalls++;
    if (failPresignOnCall != null && presignCalls == failPresignOnCall) {
      return null;
    }
    return {
      'uploadUrl': 'https://upload.example/$presignCalls',
      'accessUrl': 'https://cdn.example/$filename',
      'objectKey': 'media/$filename',
    };
  }

  @override
  Future<bool> uploadToOss(
    String uploadUrl,
    Uint8List fileBytes, {
    String? contentType,
  }) async {
    return true;
  }
}

void main() {
  late _RecordingImService imService;
  late _FakeOssService ossService;
  late Directory tempDir;

  setUp(() async {
    Get.reset();
    imService = _RecordingImService();
    ossService = _FakeOssService();
    Get.put<ImService>(imService);
    Get.put<OssService>(ossService);
    tempDir = await Directory.systemTemp.createTemp('share_outbound_test');
  });

  tearDown(() async {
    Get.reset();
    if (tempDir.existsSync()) {
      await tempDir.delete(recursive: true);
    }
  });

  Future<String> writeTempFile(String name, List<int> bytes) async {
    final file = File('${tempDir.path}/$name');
    await file.writeAsBytes(bytes);
    return file.path;
  }

  test('long text attachment bytes are UTF-8 and round-trip with emoji', () {
    final text = '${'中' * 5000}🎉${'文' * 5001}';
    expect(text.length, greaterThan(ShareOutboundSender.maxShareTextCharacters));

    final bytes = ShareOutboundSender.encodeTextAsUtf8AttachmentBytes(text);
    expect(utf8.decode(bytes), text);
  });

  test('sends one message with caption, shared text, and multiple files', () async {
    final docPath = await writeTempFile('a.txt', utf8.encode('hello file'));
    final manifest = ShareInboxManifest(
      id: 'm1',
      createdAt: 0,
      items: [
        const ShareInboxItem(type: 'text', text: 'shared line'),
        ShareInboxItem(
          type: 'file',
          fileName: 'a.txt',
          mime: 'text/plain',
          absolutePath: docPath,
        ),
        ShareInboxItem(
          type: 'file',
          fileName: 'b.txt',
          mime: 'text/plain',
          absolutePath: await writeTempFile('b.txt', utf8.encode('second')),
        ),
      ],
    );

    final sender = ShareOutboundSender(
      imService: imService,
      ossService: ossService,
    );
    await sender.sendManifestToSession(
      manifest: manifest,
      sessionId: 'sess-1',
      caption: 'my note',
    );

    expect(imService.sendCount, 1);
    expect(imService.lastSessionId, 'sess-1');
    expect(imService.lastExtra, isNotNull);
    expect(imService.lastContent, isNotNull);
    expect(
      imService.lastContent,
      startsWith('my note\nshared line\n'),
    );
    expect(imService.lastContent!.contains('https://cdn.example/a.txt'), isTrue);
    expect(imService.lastContent!.contains('https://cdn.example/b.txt'), isTrue);
    expect(ossService.presignCalls, 2);
  });

  test('spills shared text over 10k chars into txt on the same message', () async {
    final longText = 'x' * (ShareOutboundSender.maxShareTextCharacters + 1);
    final manifest = ShareInboxManifest(
      id: 'm2',
      createdAt: 0,
      items: [ShareInboxItem(type: 'text', text: longText)],
    );

    final sender = ShareOutboundSender(
      imService: imService,
      ossService: ossService,
    );
    await sender.sendManifestToSession(
      manifest: manifest,
      sessionId: 'sess-2',
      caption: 'only caption',
    );

    expect(imService.sendCount, 1);
    expect(imService.lastContent, startsWith('only caption\n'));
    expect(imService.lastContent, isNot(contains(longText)));
    expect(imService.lastExtra, isNotNull);
    expect(ossService.presignCalls, 1);
  });

  test('file:// text is not sent as message body alongside the real file', () async {
    final docPath = await writeTempFile('doc.pdf', utf8.encode('%PDF-1.4'));
    final manifest = ShareInboxManifest(
      id: 'm3',
      createdAt: 0,
      items: [
        ShareInboxItem(
          type: 'text',
          text: 'file://$docPath',
        ),
        ShareInboxItem(
          type: 'file',
          fileName: 'doc.pdf',
          mime: 'application/pdf',
          absolutePath: docPath,
        ),
      ],
    );

    final sender = ShareOutboundSender(
      imService: imService,
      ossService: ossService,
    );
    await sender.sendManifestToSession(
      manifest: manifest,
      sessionId: 'sess-3',
    );

    expect(imService.sendCount, 1);
    expect(imService.lastContent, contains('https://cdn.example/doc.pdf'));
    expect(imService.lastContent, isNot(contains('file://')));
  });

  test('upload failure does not send a message', () async {
    ossService = _FakeOssService(failPresignOnCall: 2);
    Get.put<OssService>(ossService);

    final manifest = ShareInboxManifest(
      id: 'm4',
      createdAt: 0,
      items: [
        ShareInboxItem(
          type: 'file',
          fileName: 'one.txt',
          mime: 'text/plain',
          absolutePath: await writeTempFile('one.txt', utf8.encode('1')),
        ),
        ShareInboxItem(
          type: 'file',
          fileName: 'two.txt',
          mime: 'text/plain',
          absolutePath: await writeTempFile('two.txt', utf8.encode('2')),
        ),
      ],
    );

    final sender = ShareOutboundSender(
      imService: imService,
      ossService: ossService,
    );

    await expectLater(
      sender.sendManifestToSession(manifest: manifest, sessionId: 'sess-4'),
      throwsA(isA<ShareOutboundSendFailure>()),
    );
    expect(imService.sendCount, 0);
    expect(ossService.presignCalls, 2);
  });
}
