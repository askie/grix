import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/profile/profile_local_store.dart';
import 'package:path/path.dart' as p;

void main() {
  late Directory tempDir;

  setUp(() async {
    tempDir = await Directory.systemTemp.createTemp('profile_store_test');
  });

  tearDown(() async {
    ProfileLocalStore.resetForTest();
    if (await tempDir.exists()) {
      await tempDir.delete(recursive: true);
    }
  });

  File storeFile() => File(p.join(tempDir.path, 'auth_session.json'));

  test('读写字符串/整数/布尔并落盘可重载', () async {
    final store = await ProfileLocalStore.open(storeFile());
    await store.set('access_token', 'tok-1');
    await store.set('access_expires_at_ms', 12345);
    await store.set('username_modified', true);

    final reloaded = await ProfileLocalStore.open(storeFile());
    expect(reloaded.getString('access_token'), 'tok-1');
    expect(reloaded.getInt('access_expires_at_ms'), 12345);
    expect(reloaded.getBool('username_modified'), isTrue);
  });

  test('set null / remove 删除键', () async {
    final store = await ProfileLocalStore.open(storeFile());
    await store.set('k1', 'v1');
    await store.set('k2', 'v2');
    await store.set('k1', null);
    await store.remove('k2');

    final reloaded = await ProfileLocalStore.open(storeFile());
    expect(reloaded.getString('k1'), isNull);
    expect(reloaded.getString('k2'), isNull);
    expect(reloaded.containsKey('k1'), isFalse);
  });

  test('原子替换：目标文件已存在时覆盖写入（Windows rename 语义回归点）', () async {
    final store = await ProfileLocalStore.open(storeFile());
    await store.set('k', 'v1');
    // 第二次写入走"目标已存在"路径
    await store.set('k', 'v2');
    await store.set('k2', 'v3');

    final content =
        jsonDecode(await storeFile().readAsString()) as Map<String, dynamic>;
    expect(content['k'], 'v2');
    expect(content['k2'], 'v3');
    // 临时文件不残留
    expect(File('${storeFile().path}.tmp').existsSync(), isFalse);
  });

  test('损坏的 json 按空库处理，不抛异常', () async {
    await storeFile().writeAsString('{ not valid json !!!');
    final store = await ProfileLocalStore.open(storeFile());
    expect(store.getString('anything'), isNull);
    // 仍可正常写入恢复
    await store.set('k', 'v');
    final reloaded = await ProfileLocalStore.open(storeFile());
    expect(reloaded.getString('k'), 'v');
  });

  test('getInt 兼容字符串数字', () async {
    final store = await ProfileLocalStore.open(storeFile());
    await store.set('n', '42');
    expect(store.getInt('n'), 42);
  });

  test('并发写串行落盘，最终状态一致', () async {
    final store = await ProfileLocalStore.open(storeFile());
    await Future.wait([
      for (var i = 0; i < 50; i++) store.set('key_$i', 'value_$i'),
    ]);
    final reloaded = await ProfileLocalStore.open(storeFile());
    for (var i = 0; i < 50; i++) {
      expect(reloaded.getString('key_$i'), 'value_$i');
    }
  });
}
