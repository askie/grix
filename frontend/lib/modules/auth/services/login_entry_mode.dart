enum LoginEntryMode {
  normal,
  qrScan,
}

LoginEntryMode resolveLoginEntryMode({
  required Map<String, String?> routeParameters,
  required Uri baseUri,
}) {
  final sidFromRoute = routeParameters['sid']?.trim() ?? '';
  final qtFromRoute = routeParameters['qt']?.trim() ?? '';
  if (sidFromRoute.isNotEmpty && qtFromRoute.isNotEmpty) {
    return LoginEntryMode.qrScan;
  }

  final sidFromBaseUri = baseUri.queryParameters['sid']?.trim() ?? '';
  final qtFromBaseUri = baseUri.queryParameters['qt']?.trim() ?? '';
  if (sidFromBaseUri.isNotEmpty && qtFromBaseUri.isNotEmpty) {
    return LoginEntryMode.qrScan;
  }

  return LoginEntryMode.normal;
}
