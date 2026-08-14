// Stub for non-web platforms. The conditional import in local_llm_service.dart
// swaps this out for web_fetch_stream_web.dart on web targets.

// Returns an empty stream. This should never be called on native platforms.
Stream<String> fetchStreamingText(String url, String body) {
  throw UnsupportedError('fetchStreamingText is only available on web');
}
