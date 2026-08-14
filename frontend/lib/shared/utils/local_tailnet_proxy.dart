// 媒体播放器 URL 改写入口：把 tailnet 自签 HTTPS 媒体地址换成本机 loopback
// 反代地址（http://127.0.0.1:<port>/u?t=<base64url>），让原生播放器
// (iOS AVPlayer / Android ExoPlayer / macOS AVPlayer) 不再直面自签证书。
//
// dart:io 平台 → 走 _io 实现（真正起 HttpServer 反代）；
// Web 平台   → 走 _stub 实现（直接返回原 URI）。
export 'local_tailnet_proxy_stub.dart'
    if (dart.library.io) 'local_tailnet_proxy_io.dart';
