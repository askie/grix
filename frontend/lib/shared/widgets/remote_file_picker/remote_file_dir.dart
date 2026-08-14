// 平台条件导出：原生用 dart:io 建目录，Web 用桩实现。
// 远程文件下载到本机目录是原生（手机/桌面）能力，Web 不支持。
export 'remote_file_dir_stub.dart'
    if (dart.library.io) 'remote_file_dir_io.dart';
