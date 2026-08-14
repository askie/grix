import 'home_tab_url_sync_base.dart';
import 'home_tab_url_sync_stub.dart'
    if (dart.library.js_interop) 'home_tab_url_sync_web.dart'
    as impl;

export 'home_tab_url_sync_base.dart';

HomeTabUrlSync createHomeTabUrlSync() => impl.createHomeTabUrlSync();
