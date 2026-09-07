import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// 更新流程的每个失败态都直接把 i18n key 当正文渲染（`_failureKey.tr`），
/// 少一个 key 用户就会看到 "update_integrity_failed" 这样的原文而不是提示语。
/// 这条用例把源码里引用的 update_* key 与语言文件对齐，漏加时测试直接红。
void main() {
  const sources = [
    'lib/data/providers/app_update_service.dart',
    'lib/data/providers/android_update_support.dart',
  ];
  const locales = ['assets/i18n/zh_CN.json', 'assets/i18n/en_US.json'];

  // 只认单引号字符串字面量里的 update_ 开头 key，避免把注释里的散文吃进来；
  // 前面紧跟 `[` 的排除掉——那是 JSON 字段名（latest['update_method']），
  // 不是 i18n key。
  final keyPattern = RegExp(r"(?<!\[)'(update_[a-z0-9_]+)'");

  test('代码里引用的所有 update_* key 在中英文语言文件里都存在', () {
    final referenced = <String>{};
    for (final path in sources) {
      final file = File(path);
      expect(file.existsSync(), isTrue, reason: '找不到源码文件 $path');
      for (final m in keyPattern.allMatches(file.readAsStringSync())) {
        referenced.add(m.group(1)!);
      }
    }
    // 防止正则写坏后测试变成空跑。
    expect(referenced.length, greaterThan(10));
    expect(referenced, contains('update_integrity_failed'));

    for (final locale in locales) {
      final map = (json.decode(File(locale).readAsStringSync()) as Map)
          .cast<String, dynamic>();
      final missing = referenced.where((k) => !map.containsKey(k)).toList()
        ..sort();
      expect(missing, isEmpty, reason: '$locale 缺少这些 key: $missing');
    }
  });

  test('中英文语言文件的 update_* key 集合一致', () {
    Set<String> updateKeys(String locale) =>
        (json.decode(File(locale).readAsStringSync()) as Map).keys
            .cast<String>()
            .where((k) => k.startsWith('update_'))
            .toSet();

    final zh = updateKeys(locales[0]);
    final en = updateKeys(locales[1]);
    expect(zh.difference(en), isEmpty, reason: 'en_US 缺少这些 key');
    expect(en.difference(zh), isEmpty, reason: 'zh_CN 缺少这些 key');
  });
}
