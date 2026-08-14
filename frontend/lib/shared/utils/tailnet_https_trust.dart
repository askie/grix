// tailnet 自签 HTTPS 信任安装入口。
//
// 通过条件导入选择实现：原生平台(dart:io)安装一个全局 HttpOverrides，
// 仅对 tailnet 内、由宿主机自签 CA 签发的证书放行；Web 平台为空操作
// （浏览器有自己的证书信任体系，不需要也无法注入）。
export 'tailnet_https_trust_stub.dart'
    if (dart.library.io) 'tailnet_https_trust_io.dart';
