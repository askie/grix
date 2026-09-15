import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import '../models/share_inbox_manifest.dart';

class ShareIngestNativeBridge {
  ShareIngestNativeBridge._();

  static const MethodChannel _channel = MethodChannel('grix/share_ingest');
  static const EventChannel _events = EventChannel('grix/share_ingest_events');

  static Stream<String>? _eventStream;

  static Stream<String> shareReceivedEvents() {
    _eventStream ??= _events
        .receiveBroadcastStream()
        .map((event) => event?.toString() ?? '')
        .where((id) => id.isNotEmpty);
    return _eventStream!;
  }

  static Future<List<ShareInboxManifest>> consumePending() async {
    if (kIsWeb) {
      return const <ShareInboxManifest>[];
    }
    try {
      final raw = await _channel.invokeMethod<List<Object?>>('consumePending');
      if (raw == null || raw.isEmpty) {
        return const <ShareInboxManifest>[];
      }
      final manifests = <ShareInboxManifest>[];
      for (final entry in raw) {
        if (entry is! Map) continue;
        final map = Map<String, dynamic>.from(entry);
        final manifest = ShareInboxManifest.fromJson(map);
        if (!manifest.isPresentable) continue;
        manifests.add(manifest);
      }
      return manifests;
    } catch (error, stackTrace) {
      debugPrint('ShareIngestNativeBridge.consumePending: $error\n$stackTrace');
      return const <ShareInboxManifest>[];
    }
  }

  static Future<void> deleteEntry(String id) async {
    final normalized = id.trim();
    if (normalized.isEmpty || kIsWeb) {
      return;
    }
    try {
      await _channel.invokeMethod<void>('deleteEntry', <String, dynamic>{
        'id': normalized,
      });
    } catch (error) {
      debugPrint('ShareIngestNativeBridge.deleteEntry: $error');
    }
  }
}
