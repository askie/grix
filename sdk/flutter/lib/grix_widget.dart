/// Grix Website Widget Flutter SDK.
///
/// The SDK is a thin shell around the hosted Grix mobile widget page. Chat
/// rendering, WebSocket transport, reconnection, and token renewal stay in the
/// hosted page; this package owns the WebView, visitor-key persistence, bridge
/// callbacks, visibility notifications, and external-link handling.
library;

export 'src/grix_bridge_event.dart';
export 'src/grix_config.dart';
export 'src/grix_url_builder.dart';
export 'src/grix_visitor_store.dart';
export 'src/grix_widget.dart';
export 'src/grix_widget_controller.dart';
export 'src/grix_widget_view.dart';
