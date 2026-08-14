// Web 平台 stub：浏览器 <video> 走系统证书栈，反代无法生效，
// 直接返回原 URI（Web 用户按 SKILL.md 自行装根 CA）。
Future<Uri> rewriteTailnetMediaUrl(Uri original) async => original;
