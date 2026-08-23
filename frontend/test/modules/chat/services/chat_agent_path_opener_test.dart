import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_agent_path_opener.dart';
import 'package:grix/shared/widgets/remote_file_picker/remote_file_picker.dart';

void main() {
  Future<BuildContext> pumpContext(WidgetTester tester) async {
    late BuildContext context;
    await tester.pumpWidget(
      MaterialApp(
        home: Builder(
          builder: (value) {
            context = value;
            return const SizedBox.shrink();
          },
        ),
      ),
    );
    return context;
  }

  testWidgets('opens a directory at the linked path without probing Tailnet', (
    WidgetTester tester,
  ) async {
    final context = await pumpContext(tester);
    String? browsedPath;
    var probeCalls = 0;
    final opener = ChatAgentPathOpener(
      uploadBaseUrl: 'http://100.64.0.1:1234',
      listProvider: (parentId, query) async => const RemoteFileListResult(
        files: [
          RemoteFileNode(
            id: '/workspace/project',
            name: 'project',
            isDirectory: true,
          ),
        ],
      ),
      hostProbe: (_) async {
        probeCalls++;
        return true;
      },
      browser: (context, initialPath, uploadBaseUrl) async {
        browsedPath = initialPath;
      },
    );

    await opener.open(context, '/workspace/project');

    expect(browsedPath, '/workspace/project');
    expect(probeCalls, 0);
  });

  testWidgets('previews a supported file when Tailnet is reachable', (
    WidgetTester tester,
  ) async {
    final context = await pumpContext(tester);
    RemoteFileNode? previewedNode;
    String? browsedPath;
    final opener = ChatAgentPathOpener(
      uploadBaseUrl: 'http://100.64.0.1:1234/',
      listProvider: (parentId, query) async => const RemoteFileListResult(
        files: [
          RemoteFileNode(
            id: '/workspace/Makefile',
            name: 'Makefile',
            isDirectory: false,
            mimeType: 'text/plain',
          ),
        ],
      ),
      hostProbe: (_) async => true,
      preview: (node, uploadBaseUrl) async {
        previewedNode = node;
        expect(uploadBaseUrl, 'http://100.64.0.1:1234');
      },
      browser: (context, initialPath, uploadBaseUrl) async {
        browsedPath = initialPath;
      },
    );

    await opener.open(context, '/workspace/Makefile');

    expect(previewedNode?.id, '/workspace/Makefile');
    expect(browsedPath, isNull);
  });

  testWidgets(
    'falls back to the parent directory when Tailnet is unreachable',
    (WidgetTester tester) async {
      final context = await pumpContext(tester);
      String? browsedPath;
      String? browserBaseUrl = 'unset';
      var previewCalls = 0;
      final opener = ChatAgentPathOpener(
        uploadBaseUrl: 'http://100.64.0.1:1234',
        listProvider: (parentId, query) async => const RemoteFileListResult(
          files: [
            RemoteFileNode(
              id: '/workspace/README.md',
              name: 'README.md',
              isDirectory: false,
              mimeType: 'text/markdown',
            ),
          ],
        ),
        hostProbe: (_) async => false,
        preview: (node, uploadBaseUrl) async => previewCalls++,
        browser: (context, initialPath, uploadBaseUrl) async {
          browsedPath = initialPath;
          browserBaseUrl = uploadBaseUrl;
        },
      );

      await opener.open(context, '/workspace/README.md');

      expect(previewCalls, 0);
      expect(browsedPath, '/workspace');
      expect(browserBaseUrl, isNull);
    },
  );

  testWidgets(
    'keeps Tailnet file actions when preview fails and opens the parent',
    (WidgetTester tester) async {
      final context = await pumpContext(tester);
      String? browsedPath;
      String? browserBaseUrl;
      final opener = ChatAgentPathOpener(
        uploadBaseUrl: 'http://100.64.0.1:1234/',
        listProvider: (parentId, query) async => const RemoteFileListResult(
          files: [
            RemoteFileNode(
              id: '/workspace/README.md',
              name: 'README.md',
              isDirectory: false,
              mimeType: 'text/markdown',
            ),
          ],
        ),
        hostProbe: (_) async => true,
        preview: (node, uploadBaseUrl) async {
          throw StateError('preview failed');
        },
        browser: (context, initialPath, uploadBaseUrl) async {
          browsedPath = initialPath;
          browserBaseUrl = uploadBaseUrl;
        },
      );

      await opener.open(context, '/workspace/README.md');

      expect(browsedPath, '/workspace');
      expect(browserBaseUrl, 'http://100.64.0.1:1234');
    },
  );

  testWidgets('unsupported files open their containing directory', (
    WidgetTester tester,
  ) async {
    final context = await pumpContext(tester);
    String? browsedPath;
    var probeCalls = 0;
    final opener = ChatAgentPathOpener(
      uploadBaseUrl: 'http://100.64.0.1:1234',
      listProvider: (parentId, query) async => const RemoteFileListResult(
        files: [
          RemoteFileNode(
            id: '/workspace/archive.zip',
            name: 'archive.zip',
            isDirectory: false,
            mimeType: 'application/zip',
          ),
        ],
      ),
      hostProbe: (_) async {
        probeCalls++;
        return true;
      },
      browser: (context, initialPath, uploadBaseUrl) async {
        browsedPath = initialPath;
      },
    );

    await opener.open(context, '/workspace/archive.zip');

    expect(browsedPath, '/workspace');
    expect(probeCalls, 0);
  });
}
