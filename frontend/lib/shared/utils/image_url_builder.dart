String appendVersionQueryParameter(
  String url,
  int version, {
  String key = 'v',
}) {
  final normalizedUrl = url.trim();
  if (normalizedUrl.isEmpty || version <= 0) {
    return normalizedUrl;
  }

  final normalizedKey = key.trim().isEmpty ? 'v' : key.trim();
  try {
    final uri = Uri.parse(normalizedUrl);
    final query = Map<String, String>.from(uri.queryParameters)
      ..[normalizedKey] = version.toString();
    return uri.replace(queryParameters: query).toString();
  } catch (_) {
    final separator = normalizedUrl.contains('?') ? '&' : '?';
    return '$normalizedUrl$separator$normalizedKey=$version';
  }
}
