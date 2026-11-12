import 'grix_config.dart';

/// Builds the hosted app-channel URL.
///
/// Every bootstrap value is placed in the URL fragment. The fragment is never
/// sent to the server in the HTTP request, so visitor keys and PII stay out of
/// access logs.
class GrixUrlBuilder {
  const GrixUrlBuilder._();

  static String buildAppUrl({
    required GrixConfig config,
    required String hostPackageName,
    String? visitorKey,
  }) {
    final params = <String, String>{
      'site_key': config.normalizedSiteKey,
      'platform': 'flutter',
      'app_id': config.resolveAppId(hostPackageName),
      'sdk_version': GrixConfig.sdkVersion,
    };

    void addOptional(String key, String? rawValue) {
      final value = rawValue?.trim() ?? '';
      if (value.isNotEmpty) params[key] = value;
    }

    addOptional('visitor_key', visitorKey);
    addOptional('locale', config.locale);
    addOptional('theme_color', config.themeColor);
    addOptional('title', config.title);
    addOptional('visitor_name', config.user?.name);
    addOptional('visitor_email', config.user?.email);

    final fragment = params.entries
        .map((entry) {
          final key = Uri.encodeComponent(entry.key);
          final value = Uri.encodeComponent(entry.value);
          return '$key=$value';
        })
        .join('&');
    return '${config.normalizedApiBase}/public/widget/app.html#$fragment';
  }
}
