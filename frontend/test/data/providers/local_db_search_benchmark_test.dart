import 'dart:math';

import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/local_db.dart';

/// 手动基准：往本地 sqlite 灌 5 万条消息（10% 是 3000 字长 markdown）+ 500 条
/// 会话，测量单关键词 / 双关键词 × 常见词（命中多）/ 罕见词（命中少）四种
/// 场景耗时。
///
/// 这份基准曾经用于回答"要不要给 messages(created_at DESC) 加索引"：
/// 实测该索引对这条 SQL 无效——`EXPLAIN QUERY PLAN` 显示排序仍然
/// `SCAN messages` + `USE TEMP B-TREE FOR ORDER BY`，因为 `ORDER BY` 首列是
/// 计算列 `hits`（命中数排序），不是 `created_at`，sqlite 用不上按
/// `created_at` 建的索引；对照测算（同一基准分别在有/无该索引下跑）四种
/// 场景耗时都在噪声范围内、没有收益，因此未采用。保留这个文件作为以后继续
/// 调优搜索性能时的可重复基准。
///
/// 默认跳过，不进 CI / 常规 `flutter test`；手动执行：
///   flutter test test/data/providers/local_db_search_benchmark_test.dart \
///     --dart-define=RUN_SEARCH_BENCHMARK=true
void main() {
  const runBenchmark = bool.fromEnvironment('RUN_SEARCH_BENCHMARK');
  const testUserId = 'search-benchmark-user';
  const sessionCount = 500;
  const messageCount = 50000;
  const commonWord = '会议纪要';
  const rareWord = '罕见关键字ZZ';
  const fillerWords = ['项目', '报价', '装修', '进度', '合同', '发票', '客户', '预约'];

  test(
    '搜索基准：5万消息+500会话，单/双关键词×常见/罕见词耗时',
    () async {
      final random = Random(42);
      String filler() => fillerWords[random.nextInt(fillerWords.length)];
      String buildContent(String keyword, int targetLen) {
        final buffer = StringBuffer(keyword);
        while (buffer.length < targetLen) {
          buffer.write(filler());
          buffer.write('，');
        }
        return buffer.toString();
      }

      await LocalDb.setActiveUser(testUserId);
      await LocalDb.clearActiveUserData();

      for (var i = 0; i < sessionCount; i++) {
        final title = i % 20 == 0 ? '$commonWord群$i' : '${filler()}群$i';
        await LocalDb.upsertSession({
          'session_id': 's-$i',
          'title': title,
          'type': 'group',
          'peer_id': '',
          'peer_type': 0,
          'peer_nickname': '',
          'peer_username': '',
          'updated_at': 1000 + i,
          'unread_count': 0,
          'last_message': '',
          'last_message_time': 1000 + i,
        });
      }

      const chunkSize = 2000;
      var buffer = <Map<String, dynamic>>[];
      Future<void> flush() async {
        if (buffer.isEmpty) return;
        await LocalDb.batchUpsertMessages(buffer);
        buffer = [];
      }

      for (var i = 0; i < messageCount; i++) {
        final isLong = i % 10 == 0; // 10% 长 markdown
        final targetLen = isLong ? 3000 : 500;
        final hitsRare = i == messageCount ~/ 2; // 恰好 1 条命中罕见词
        final hitsCommon = !hitsRare && i % 20 == 0; // ~5% 命中常见词
        final keyword = hitsRare
            ? rareWord
            : (hitsCommon ? commonWord : filler());
        buffer.add({
          'msg_id': 'm-$i',
          'session_id': 's-${i % sessionCount}',
          'sender_id': 'u1',
          'sender_type': 1,
          'msg_type': 1,
          'content': buildContent(keyword, targetLen),
          'status': 'sent',
          'created_at': 1000 + i,
        });
        if (buffer.length >= chunkSize) {
          await flush();
        }
      }
      await flush();

      Future<int> time(Future<void> Function() run) async {
        final sw = Stopwatch()..start();
        await run();
        sw.stop();
        return sw.elapsedMilliseconds;
      }

      final results = <String, int>{
        'single_common': await time(
          () => LocalDb.searchMessages([commonWord]),
        ),
        'single_rare': await time(() => LocalDb.searchMessages([rareWord])),
        'double_common': await time(
          () => LocalDb.searchMessages([commonWord, fillerWords.first]),
        ),
        'double_rare': await time(
          () => LocalDb.searchMessages([rareWord, fillerWords.first]),
        ),
      };
      // ignore: avoid_print
      print('=== 搜索基准（未加 created_at 索引：实测对该 SQL 无效，未采用） ===');
      for (final entry in results.entries) {
        // ignore: avoid_print
        print('  ${entry.key}: ${entry.value}ms');
      }

      await LocalDb.clearActiveUserData();
      await LocalDb.setActiveUser(null);
    },
    skip: runBenchmark
        ? false
        : '基准测试默认跳过；手动执行：flutter test <this file> '
              '--dart-define=RUN_SEARCH_BENCHMARK=true',
    timeout: const Timeout(Duration(minutes: 5)),
  );
}
