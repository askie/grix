import 'package:get/get.dart';

/// 目录绑定指令消息（grix://open/session?cwd=...）的识别与友好文案。
///
/// 该指令消息由目录绑定卡片或空白页快捷绑定组件代用户发出，
/// 原文是一条机器可读的 URI；所有面向用户的展示位（气泡、引用、
/// 会话列表预览、自动标题）都应转成友好文案，不暴露原始链接。
class ChatBindDirectoryMessage {
  const ChatBindDirectoryMessage._();

  static const String _scheme = 'grix';
  static const String _host = 'open';
  static const String _path = 'session';

  /// 从目录绑定消息 URI 中解出 cwd；不是绑定 URI 或缺 cwd 时返回空串。
  static String tryParseCwd(String raw) {
    final trimmed = raw.trim();
    if (trimmed.isEmpty || !trimmed.startsWith('$_scheme://')) return '';
    final parsed = Uri.tryParse(trimmed);
    if (parsed == null) return '';
    if (parsed.scheme.toLowerCase() != _scheme ||
        parsed.host.toLowerCase() != _host) {
      return '';
    }
    final path = parsed.path.replaceAll('/', '').trim();
    if (path != _path) return '';
    return (parsed.queryParameters['cwd'] ?? '').trim();
  }

  /// 聊天气泡/引用里的友好文案（带完整路径）；非绑定消息返回空串。
  static String friendlyText(String raw) {
    final cwd = tryParseCwd(raw);
    if (cwd.isEmpty) return '';
    return _label(cwd);
  }

  /// 会话列表预览/自动标题用的短文案（只带目录名）；非绑定消息返回空串。
  static String friendlyShortText(String raw) {
    final cwd = tryParseCwd(raw);
    if (cwd.isEmpty) return '';
    return _label(_basename(cwd));
  }

  static String _label(String path) {
    final translated = 'chat_bind_directory_message'.trParams({'path': path});
    if (translated.isEmpty || translated == 'chat_bind_directory_message') {
      return '绑定目录 $path';
    }
    return translated;
  }

  static String _basename(String path) {
    final segments = path
        .replaceAll('\\', '/')
        .split('/')
        .where((segment) => segment.trim().isNotEmpty)
        .toList(growable: false);
    if (segments.isEmpty) return path;
    return segments.last;
  }
}
