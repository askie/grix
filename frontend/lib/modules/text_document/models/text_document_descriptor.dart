enum TextDocumentSource {
  androidContentUri,
  iosSecurityScopedUrl,
  localFile,
  remoteAttachment,
  remoteFileBrowser,
  importedCopy,
}

class TextDocumentDescriptor {
  const TextDocumentDescriptor({
    required this.handle,
    required this.displayName,
    required this.mimeType,
    required this.canWrite,
    required this.source,
    this.byteLength,
    this.modifiedAt,
  });

  final String handle;
  final String displayName;
  final String mimeType;
  final bool canWrite;
  final TextDocumentSource source;
  final int? byteLength;
  final DateTime? modifiedAt;

  String get extension {
    final dot = displayName.lastIndexOf('.');
    if (dot < 0 || dot == displayName.length - 1) return '';
    return displayName.substring(dot + 1).toLowerCase();
  }

  factory TextDocumentDescriptor.fromNativeMap(Map<Object?, Object?> map) {
    final sourceName = map['source']?.toString() ?? '';
    return TextDocumentDescriptor(
      handle: map['handle']?.toString() ?? '',
      displayName: map['displayName']?.toString() ?? 'document.txt',
      mimeType: map['mimeType']?.toString() ?? 'text/plain',
      canWrite: map['canWrite'] == true,
      source: TextDocumentSource.values.firstWhere(
        (value) => value.name == sourceName,
        orElse: () => TextDocumentSource.importedCopy,
      ),
      byteLength: (map['byteLength'] as num?)?.toInt(),
      modifiedAt: switch ((map['modifiedAt'] as num?)?.toInt()) {
        final value? => DateTime.fromMillisecondsSinceEpoch(value),
        null => null,
      },
    );
  }
}
