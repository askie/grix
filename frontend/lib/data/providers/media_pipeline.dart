import 'dart:io';
import 'package:flutter/foundation.dart';
import 'package:crypto/crypto.dart';
import 'package:image_picker/image_picker.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';
import 'local_db.dart';
// import 'package:dio/dio.dart'; // 示例中用以发出 Http 上传

class MediaPipeline {
  /// 核心流水线：端侧压切 -> 哈希计算 -> 获取直传 STS -> 旁路直传
  static Future<String?> processAndUploadImage(
      XFile originalFile, Function(double)? onProgress) async {
    File? scopedTempFile;
    try {
      final userId = LocalDb.activeUserId;
      if (userId == null) {
        debugPrint('Media Pipeline Error: active user is not set');
        return null;
      }

      // 1. 端侧降解，防止发送原图导致内存和长连接崩盘
      // 注意：真实场景中通常使用 flutter_image_compress 对 file 进行操作。此处做预留架子。
      final processedFile = originalFile; // 模拟压缩后的文件

      // 2. 特征值提取计算
      final bytes = await processedFile.readAsBytes();
      final hash = md5.convert(bytes).toString();

      // 3. 向业务服务器请求上传 Token / 秒传
      final uploadInfo = await _fetchUploadToken(hash);

      // 如果服务端返回了已存在的 URL (秒传命中)
      if (uploadInfo['is_exist'] == true) {
        return uploadInfo['url'];
      }

      // 4. 真正实施不经过长连接的 OSS 直传
      final String ossUrl = uploadInfo['upload_url'];
      scopedTempFile = await _copyToUserScopedTempFile(
        bytes: bytes,
        sourcePath: processedFile.path,
        userId: userId,
      );
      await _putToOss(ossUrl, scopedTempFile, onProgress);

      // 5. 组装返回最终对象标记
      return uploadInfo['final_asset_url'];
    } catch (e) {
      debugPrint('Media Pipeline Error: $e');
      return null;
    } finally {
      if (scopedTempFile != null && await scopedTempFile.exists()) {
        await scopedTempFile.delete();
      }
    }
  }

  static Future<Map<String, dynamic>> _fetchUploadToken(String hash) async {
    // 模拟 RESTful API 调用: /v1/upload/token
    await Future.delayed(const Duration(milliseconds: 300));
    return {
      'is_exist': false,
      'upload_url': 'https://oss.example.com/put?token=TempSTS',
      'final_asset_url': 'https://assets.grix.com/img_$hash.jpg'
    };
  }

  static Future<void> _putToOss(
      String putUrl, File file, Function(double)? onProgress) async {
    // 模拟耗时的分片上传与进度返回
    for (int i = 1; i <= 5; i++) {
      await Future.delayed(const Duration(milliseconds: 200));
      if (onProgress != null) onProgress(i / 5.0);
    }
  }

  static Future<File> _copyToUserScopedTempFile({
    required List<int> bytes,
    required String sourcePath,
    required String userId,
  }) async {
    final rootDir = await getTemporaryDirectory();
    final userMediaDir = Directory(path.join(
      rootDir.path,
      'grix',
      'users',
      userId,
      'media',
    ));
    if (!await userMediaDir.exists()) {
      await userMediaDir.create(recursive: true);
    }

    final ext = path.extension(sourcePath);
    final fileName =
        'upload_${DateTime.now().microsecondsSinceEpoch}${ext.isEmpty ? '.bin' : ext}';
    final filePath = path.join(userMediaDir.path, fileName);
    final file = File(filePath);
    await file.writeAsBytes(bytes, flush: true);
    return file;
  }
}
