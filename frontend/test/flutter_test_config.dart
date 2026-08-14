import 'dart:async';
import 'dart:convert';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/local_db.dart';

/// flutter test 自动执行此文件（test/flutter_test_config.dart）。
/// 预加载 en_US 翻译 asset，写入 AppTranslations._testKeys，
/// 使测试里 AppTranslations() 无参构造能返回真实英文翻译。
Future<void> testExecutable(FutureOr<void> Function() testMain) async {
  TestWidgetsFlutterBinding.ensureInitialized();
  // 测试态关闭 sqlite fsync：消除慢盘（WSL2 自建 runner 等）每事务落盘开销，
  // 避免 DB 密集型/压力用例在慢机上超时。生产代码不受影响（默认 false）。
  LocalDb.useTestFastPragmas = true;
  try {
    final rawEn = await rootBundle.loadString('assets/i18n/en_US.json');
    final decodedEn = json.decode(rawEn) as Map<String, dynamic>;
    final enUS = decodedEn.map((k, v) => MapEntry(k, v.toString()));
    final rawZh = await rootBundle.loadString('assets/i18n/zh_CN.json');
    final decodedZh = json.decode(rawZh) as Map<String, dynamic>;
    final zhCN = decodedZh.map((k, v) => MapEntry(k, v.toString()));
    AppTranslations.testKeys = {
      'en_US': enUS,
      'en': enUS,
      'zh_CN': zhCN,
      'zh': zhCN,
    };
  } catch (_) {
    // asset 加载失败时静默跳过，测试里 .tr 返回 key 本身
  }
  await testMain();
}
