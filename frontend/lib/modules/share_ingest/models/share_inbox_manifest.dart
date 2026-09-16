import 'dart:convert';

class ShareInboxItem {
  const ShareInboxItem({
    required this.type,
    this.text,
    this.path,
    this.fileName,
    this.mime,
    this.size,
    this.absolutePath,
  });

  final String type;
  final String? text;
  final String? path;
  final String? fileName;
  final String? mime;
  final int? size;

  /// Resolved on device after native ingest (not in persisted JSON).
  final String? absolutePath;

  bool get isText => type == 'text';
  bool get isUrl => type == 'url';
  bool get isFile => type == 'file';

  Map<String, dynamic> toJson() => <String, dynamic>{
    'type': type,
    if (text != null) 'text': text,
    if (path != null) 'path': path,
    if (fileName != null) 'file_name': fileName,
    if (mime != null) 'mime': mime,
    if (size != null) 'size': size,
  };

  factory ShareInboxItem.fromJson(Map<String, dynamic> json) {
    return ShareInboxItem(
      type: json['type']?.toString() ?? 'file',
      text: json['text']?.toString(),
      path: json['path']?.toString(),
      fileName: json['file_name']?.toString(),
      mime: json['mime']?.toString(),
      size: _parseInt(json['size']),
      absolutePath: json['absolute_path']?.toString(),
    );
  }

  ShareInboxItem withAbsolutePath(String absolutePath) {
    return ShareInboxItem(
      type: type,
      text: text,
      path: path,
      fileName: fileName,
      mime: mime,
      size: size,
      absolutePath: absolutePath,
    );
  }

  static int? _parseInt(dynamic value) {
    if (value is int) return value;
    if (value is num) return value.toInt();
    return int.tryParse(value?.toString() ?? '');
  }
}

class ShareInboxManifest {
  const ShareInboxManifest({
    required this.id,
    required this.createdAt,
    required this.items,
    this.source,
    this.skippedCount = 0,
    this.offeredTypes = const <String>[],
  });

  final String id;
  final int createdAt;
  final List<ShareInboxItem> items;
  final String? source;
  final int skippedCount;

  /// Type identifiers the sending app offered, recorded for diagnosing shares
  /// that arrive without usable content.
  final List<String> offeredTypes;

  bool get isPresentable => id.isNotEmpty && (items.isNotEmpty || skippedCount > 0);

  Map<String, dynamic> toJson() => <String, dynamic>{
    'id': id,
    'created_at': createdAt,
    'items': items.map((e) => e.toJson()).toList(),
    if (source != null && source!.isNotEmpty) 'source': source,
    if (skippedCount > 0) 'skipped_count': skippedCount,
  };

  factory ShareInboxManifest.fromJson(Map<String, dynamic> json) {
    final rawItems = json['items'];
    final items = <ShareInboxItem>[];
    if (rawItems is List) {
      for (final entry in rawItems) {
        if (entry is Map) {
          items.add(
            ShareInboxItem.fromJson(Map<String, dynamic>.from(entry)),
          );
        }
      }
    }
    return ShareInboxManifest(
      id: json['id']?.toString() ?? '',
      createdAt: ShareInboxItem._parseInt(json['created_at']) ?? 0,
      items: items,
      source: json['source']?.toString(),
      skippedCount: ShareInboxItem._parseInt(json['skipped_count']) ?? 0,
      offeredTypes: (json['offered_types'] is List)
          ? (json['offered_types'] as List)
                .map((e) => e.toString())
                .where((e) => e.isNotEmpty)
                .toList(growable: false)
          : const <String>[],
    );
  }

  static ShareInboxManifest? tryParseJson(String raw) {
    try {
      final decoded = jsonDecode(raw);
      if (decoded is! Map) return null;
      return ShareInboxManifest.fromJson(Map<String, dynamic>.from(decoded));
    } catch (_) {
      return null;
    }
  }
}
