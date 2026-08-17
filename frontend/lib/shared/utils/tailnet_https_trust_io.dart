import 'dart:io';

import 'tailnet_cert_pin.dart';

/// 安装全局 HttpOverrides，让 App 信任宿主机的 tailnet HTTPS 文件服务证书。
///
/// 作用域严格限定在 tailnet，对 App 访问其它网络零影响：
/// - 仅当对端是 tailnet 段(100.64.0.0/10) 且证书通过 Grix CA 标识 +
///   TOFU 指纹钉扎（见 tailnet_cert_pin.dart）时，才放行该自签证书；
/// - 其余任何地址的证书一律走系统默认校验，这里不做任何放行。
void installTailnetHttpsTrust() {
  HttpOverrides.global = _TailnetHttpOverrides();
}

class _TailnetHttpOverrides extends HttpOverrides {
  @override
  HttpClient createHttpClient(SecurityContext? context) {
    final client = super.createHttpClient(context);
    // badCertificateCallback 只在证书默认校验失败时触发：
    // - 外部网站正常证书：校验通过，不会进这里，行为不变；
    // - 外部网站异常证书：进这里，host 非 tailnet → 返回 false，照常拒绝；
    // - tailnet 宿主机自签证书：进这里，host 是 tailnet 且指纹钉扎通过 → 放行。
    client.badCertificateCallback = (X509Certificate cert, String host, int port) {
      return trustTailnetSelfSignedCert(cert, host);
    };
    return client;
  }
}
