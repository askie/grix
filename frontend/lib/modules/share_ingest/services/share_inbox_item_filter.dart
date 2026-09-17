import '../models/share_inbox_manifest.dart';

/// Filters share-inbox items before outbound send.
class ShareInboxItemFilter {
  const ShareInboxItemFilter._();

  /// WeChat and similar apps often attach a file and also expose the same path
  /// as plain text (`file://...`). Sending that text duplicates the file body.
  static bool isFileUrlTextItem(ShareInboxItem item) {
    if (!item.isText && !item.isUrl) {
      return false;
    }
    final text = item.text?.trim() ?? '';
    return text.toLowerCase().startsWith('file://');
  }

  static bool shouldIncludeTextInMessageBody(ShareInboxItem item) {
    if (!item.isText && !item.isUrl) {
      return false;
    }
    final text = item.text?.trim() ?? '';
    if (text.isEmpty) {
      return false;
    }
    return !isFileUrlTextItem(item);
  }

  static String collectSharedTextBody(List<ShareInboxItem> items) {
    final parts = <String>[];
    for (final item in items) {
      if (!shouldIncludeTextInMessageBody(item)) {
        continue;
      }
      parts.add(item.text!.trim());
    }
    return parts.join('\n');
  }
}
