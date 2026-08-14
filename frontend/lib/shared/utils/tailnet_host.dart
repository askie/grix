// Tailscale CGNAT 段判断：100.64.0.0/10 (100.64.0.0 - 100.127.255.255)。
// 在 trust 和反代两侧共用同一份判断，避免实现分叉。
//
// 严格 IPv4：四段都必须是 0-255 的整数，否则返回 false。
// 这里不接受 IPv6（含 ts.net 的 fd7a:* ULA），未来如需支持再扩展。
bool isTailnetHostString(String host) {
  final parts = host.split('.');
  if (parts.length != 4) return false;
  final octets = <int>[];
  for (final s in parts) {
    final n = int.tryParse(s);
    if (n == null || n < 0 || n > 255) return false;
    octets.add(n);
  }
  return octets[0] == 100 && octets[1] >= 64 && octets[1] <= 127;
}
