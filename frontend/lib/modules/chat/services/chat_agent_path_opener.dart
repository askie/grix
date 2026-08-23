import 'package:flutter/material.dart';

import '../../../data/providers/user_favorite_path_service.dart';
import '../../../shared/markdown/chat_markdown_uri_policy.dart';
import '../../../shared/services/remote_file_host_connectivity.dart';
import '../../../shared/widgets/remote_file_picker/remote_file_picker.dart';
import '../../text_document/services/text_document_open_service.dart';

typedef ChatAgentPathBrowser =
    Future<void> Function(
      BuildContext context,
      String initialPath,
      String? uploadBaseUrl,
    );
typedef ChatAgentPathPreview =
    Future<void> Function(RemoteFileNode node, String uploadBaseUrl);

class ChatAgentPathOpener {
  ChatAgentPathOpener({
    required this.listProvider,
    required this.uploadBaseUrl,
    this.hostProbe = RemoteFileHostConnectivity.isReachable,
    this.browser,
    this.preview,
  });

  final RemoteFileListProvider listProvider;
  final String uploadBaseUrl;
  final RemoteFileHostProbe hostProbe;
  final ChatAgentPathBrowser? browser;
  final ChatAgentPathPreview? preview;

  Future<void> open(BuildContext context, String rawPath) async {
    final path = ChatMarkdownUriPolicy.resolveAgentFilePath(rawPath);
    if (path == null) return;

    final parentPath = _parentPath(path);
    final node = await _findNode(path, parentPath);
    if (!context.mounted) return;

    if (_isDirectoryPath(path, node)) {
      await _browse(context, path, _normalizedBaseUrl);
      return;
    }

    final fileName = node?.name.trim().isNotEmpty == true
        ? node!.name.trim()
        : _basename(path);
    final mimeType = node?.mimeType?.trim() ?? '';
    final canPreview = TextDocumentOpenService.supportsRemoteFile(
      fileName: fileName,
      mimeType: mimeType,
    );
    final browsePath = parentPath ?? path;
    final baseUrl = _normalizedBaseUrl;
    if (!canPreview || baseUrl == null) {
      await _browse(context, browsePath, baseUrl);
      return;
    }

    final reachable = await hostProbe(baseUrl);
    if (!context.mounted) return;
    if (!reachable) {
      await _browse(context, browsePath, null);
      return;
    }

    final resolvedNode =
        node ??
        RemoteFileNode(
          id: path,
          name: fileName,
          isDirectory: false,
          mimeType: mimeType,
        );
    try {
      await _preview(resolvedNode, baseUrl);
    } catch (_) {
      if (!context.mounted) return;
      await _browse(context, browsePath, baseUrl);
    }
  }

  String? get _normalizedBaseUrl {
    final normalized = uploadBaseUrl.trim().replaceFirst(RegExp(r'/+$'), '');
    return normalized.isEmpty ? null : normalized;
  }

  Future<RemoteFileNode?> _findNode(String path, String? parentPath) async {
    if (parentPath == null) return null;
    try {
      final result = await listProvider(
        parentPath,
        const RemoteFileListQuery(showHidden: true),
      );
      final normalizedPath = _normalizeForComparison(path);
      for (final item in result.files) {
        if (_normalizeForComparison(item.id) == normalizedPath) {
          return item;
        }
      }

      final basename = _basename(path);
      final nameMatches = result.files
          .where((item) => item.name == basename)
          .toList(growable: false);
      return nameMatches.length == 1 ? nameMatches.single : null;
    } catch (_) {
      return null;
    }
  }

  bool _isDirectoryPath(String path, RemoteFileNode? node) {
    if (node != null) return node.isDirectory;
    final normalized = path.replaceAll('\\', '/');
    return normalized == '/' ||
        RegExp(r'^[A-Za-z]:/$').hasMatch(normalized) ||
        normalized.endsWith('/');
  }

  Future<void> _browse(
    BuildContext context,
    String initialPath,
    String? baseUrl,
  ) {
    final override = browser;
    if (override != null) {
      return override(context, initialPath, baseUrl);
    }
    return RemoteFilePicker.show(
      context,
      listProvider: listProvider,
      favoriteApi: UserFavoritePathService(),
      pickTarget: RemoteFilePickTarget.directories,
      selectionMode: RemoteFileSelectionMode.single,
      initialPath: initialPath,
      uploadBaseUrl: baseUrl,
    ).then((_) {});
  }

  Future<void> _preview(RemoteFileNode node, String baseUrl) {
    final override = preview;
    if (override != null) return override(node, baseUrl);
    return TextDocumentOpenService.openRemoteFile(
      url: '$baseUrl/download',
      fileName: node.name,
      mimeType: node.mimeType ?? '',
      queryParameters: {'path': node.id},
      handleSeed: '$baseUrl:${node.id}',
    );
  }

  static String _normalizeForComparison(String path) {
    final normalized = path.replaceAll('\\', '/');
    if (normalized.length > 1 && normalized.endsWith('/')) {
      return normalized.substring(0, normalized.length - 1);
    }
    return normalized;
  }

  static String? _parentPath(String path) {
    final normalized = _normalizeForComparison(path);
    if (normalized == '/' || RegExp(r'^[A-Za-z]:$').hasMatch(normalized)) {
      return null;
    }
    final slash = normalized.lastIndexOf('/');
    if (slash < 0) return null;
    if (slash == 0) return '/';
    if (slash == 2 && normalized[1] == ':') {
      return normalized.substring(0, 3);
    }
    return normalized.substring(0, slash);
  }

  static String _basename(String path) {
    final normalized = _normalizeForComparison(path);
    final slash = normalized.lastIndexOf('/');
    return slash < 0 ? normalized : normalized.substring(slash + 1);
  }
}
