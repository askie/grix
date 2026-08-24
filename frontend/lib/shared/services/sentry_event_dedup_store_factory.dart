import 'sentry_event_dedup_store.dart';
import 'sentry_event_dedup_store_factory_stub.dart'
    if (dart.library.io) 'sentry_event_dedup_store_factory_io.dart'
    if (dart.library.js_interop) 'sentry_event_dedup_store_factory_web.dart'
    as platform;

SentryDedupStore createSentryDedupStore() => platform.createSentryDedupStore();
