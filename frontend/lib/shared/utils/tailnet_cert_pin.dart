import 'dart:io';

import 'package:crypto/crypto.dart';

import 'tailnet_host.dart';

/// tailnet 自签证书的指纹钉扎（TOFU, trust-on-first-use）。
///
/// 背景：connector 在每台宿主机运行时生成自签 CA
/// （CN: `Grix Tailnet Local CA (<hostname>)`）并签发服务证书，CA 指纹没有任何
/// 带外下发通道（host_meta 只有 tailnet_ip/端口），无法做构建期静态 pin。
/// 旧实现只按 issuer CN 子串信任——tailnet 内任何人都能生成同名 CA 伪造证书，
/// 该校验形同虚设。
///
/// 现改为：首次见到某宿主机证书时钉住其 DER 的 SHA-256 指纹（进程内存内），
/// 后续到同一宿主机的连接指纹不一致即拒绝。首次连接仍依赖 tailnet 网段
/// 本身的可信性；connector 重启换证后需重启 App 重新 TOFU。
/// 取舍与"低危项最小改动"一致，不引入持久化与额外配置面。
final Map<String, String> _pinnedFingerprints = <String, String>{};

/// 判定是否信任 tailnet 自签证书：tailnet 网段 + Grix CA 标识 + TOFU 指纹一致。
///
/// `tailnet_https_trust_io.dart` 与 `local_tailnet_proxy_io.dart` 共用，
/// 避免两处放行规则分叉。
bool trustTailnetSelfSignedCert(X509Certificate cert, String host) {
  if (!isTailnetHostString(host)) return false;
  if (!cert.issuer.contains('Grix Tailnet Local CA')) return false;
  final fingerprint = sha256.convert(cert.der).toString();
  final pinned = _pinnedFingerprints[host];
  if (pinned == null) {
    _pinnedFingerprints[host] = fingerprint;
    return true;
  }
  return pinned == fingerprint;
}
