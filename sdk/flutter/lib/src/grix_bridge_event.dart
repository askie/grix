import 'dart:convert';

sealed class GrixBridgeEvent {
  const GrixBridgeEvent();

  static GrixBridgeEvent? parse(String raw) {
    final text = raw.trim();
    if (text.startsWith('{') == false || text.endsWith('}') == false) {
      return null;
    }
    try {
      final decoded = jsonDecode(text);
      if (decoded is Map == false) return null;
      final map = decoded.map((key, value) => MapEntry('$key', value));
      switch (map['type']) {
        case 'ready':
          return GrixReadyEvent(
            visitorKey: '${map['visitor_key'] ?? ''}',
            sessionId: '${map['session_id'] ?? ''}',
          );
        case 'close':
          return const GrixCloseEvent();
        case 'session_closed':
          final reason = '${map['reason'] ?? ''}'.trim();
          return GrixSessionClosedEvent(reason.isEmpty ? 'closed' : reason);
        case 'unread':
          final rawCount = map['count'];
          final count = rawCount is num
              ? rawCount.toInt()
              : int.tryParse('$rawCount') ?? 0;
          return GrixUnreadEvent(count < 0 ? 0 : count);
        case 'open_url':
          final url = '${map['url'] ?? ''}'.trim();
          final uri = Uri.tryParse(url);
          if (uri == null ||
              (uri.scheme.toLowerCase() != 'http' &&
                  uri.scheme.toLowerCase() != 'https')) {
            return null;
          }
          return GrixOpenUrlEvent(url);
        case 'error':
          final code = '${map['code'] ?? ''}'.trim();
          final message = '${map['message'] ?? ''}'.trim();
          return GrixErrorEvent(
            code: code.isEmpty ? 'unknown' : code,
            message: message.isEmpty ? 'Unknown error' : message,
          );
        default:
          return null;
      }
    } catch (_) {
      return null;
    }
  }
}

final class GrixReadyEvent extends GrixBridgeEvent {
  const GrixReadyEvent({required this.visitorKey, required this.sessionId});
  final String visitorKey;
  final String sessionId;
}

final class GrixCloseEvent extends GrixBridgeEvent {
  const GrixCloseEvent();
}

final class GrixSessionClosedEvent extends GrixBridgeEvent {
  const GrixSessionClosedEvent(this.reason);
  final String reason;
}

final class GrixUnreadEvent extends GrixBridgeEvent {
  const GrixUnreadEvent(this.count);
  final int count;
}

final class GrixOpenUrlEvent extends GrixBridgeEvent {
  const GrixOpenUrlEvent(this.url);
  final String url;
}

final class GrixErrorEvent extends GrixBridgeEvent {
  const GrixErrorEvent({required this.code, required this.message});
  final String code;
  final String message;
}
