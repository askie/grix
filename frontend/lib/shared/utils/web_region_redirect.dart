import 'web_region_redirect_stub.dart'
    if (dart.library.js_interop) 'web_region_redirect_web.dart'
    as impl;

import 'app_region_config.dart';

/// On web: redirects the browser to the target region's root URL when the
/// current page is on a different production domain; returns true (navigating away).
/// On native: no-op, always returns false.
bool redirectToRegionIfNeeded(AppRegion region) =>
    impl.redirectToRegionIfNeeded(region);
