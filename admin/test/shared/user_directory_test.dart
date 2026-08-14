import 'package:grix_admin/modules/users/admin_user_item.dart';
import 'package:grix_admin/shared/controllers/user_directory.dart';
import 'package:flutter_test/flutter_test.dart';

AdminUserItem _user(String id, String nickname) {
  return AdminUserItem.fromJson({
    'id': id,
    'username': 'u$id',
    'nickname': nickname,
    'status': 1,
  });
}

void main() {
  group('UserDirectory', () {
    test('同一窗口内多个ID合并为一次批量请求，未命中写负缓存', () async {
      final dir = UserDirectory();
      final calls = <List<String>>[];
      dir.lookupFn = (ids) async {
        calls.add(List.of(ids));
        return [_user('1', '张三'), _user('2', '李四')];
      };

      // 渲染期连续 resolve 三个 ID（其中 3 不存在），应只发一次请求。
      expect(dir.resolve('1'), isNull);
      expect(dir.resolve('2'), isNull);
      expect(dir.resolve('3'), isNull);
      expect(dir.resolve('1'), isNull); // 重复 resolve 不重复入队

      await Future<void>.delayed(const Duration(milliseconds: 150));

      expect(calls, hasLength(1));
      expect(calls.first.toSet(), {'1', '2', '3'});
      expect(dir.resolve('1')?.displayName, '张三');
      expect(dir.resolve('2')?.displayName, '李四');
      // 不存在的 ID：已解析（负缓存），返回 null 且不再发请求。
      expect(dir.isResolved('3'), isTrue);
      expect(dir.resolve('3'), isNull);

      await Future<void>.delayed(const Duration(milliseconds: 150));
      expect(calls, hasLength(1));
    });

    test('请求失败不写负缓存，退避后自动限次重试', () async {
      final dir = UserDirectory()..retryDelay = const Duration(milliseconds: 50);
      var failFirst = true;
      final calls = <List<String>>[];
      dir.lookupFn = (ids) async {
        calls.add(List.of(ids));
        if (failFirst) {
          failFirst = false;
          throw Exception('network');
        }
        return [_user('9', '王五')];
      };

      dir.resolve('9');
      await Future<void>.delayed(const Duration(milliseconds: 100));
      expect(dir.isResolved('9'), isFalse);

      // 不需要界面再 resolve，退避到点自动重试成功。
      await Future<void>.delayed(const Duration(milliseconds: 150));
      expect(calls, hasLength(2));
      expect(dir.resolve('9')?.displayName, '王五');
    });

    test('自动重试超限后停手，界面重建 resolve 仍可再试', () async {
      final dir = UserDirectory()..retryDelay = const Duration(milliseconds: 30);
      var failing = true;
      final calls = <List<String>>[];
      dir.lookupFn = (ids) async {
        calls.add(List.of(ids));
        if (failing) throw Exception('network');
        return [_user('8', '赵六')];
      };

      dir.resolve('8');
      // 首发 1 次 + 自动重试上限 2 次 = 3 次后不再自动打。
      await Future<void>.delayed(const Duration(milliseconds: 400));
      expect(calls, hasLength(3));
      expect(dir.isResolved('8'), isFalse);

      // 网络恢复后由界面重建触发的 resolve 再试，成功回填。
      failing = false;
      dir.resolve('8');
      await Future<void>.delayed(const Duration(milliseconds: 150));
      expect(dir.resolve('8')?.displayName, '赵六');
    });

    test('invalidate 后重新拉取', () async {
      final dir = UserDirectory();
      var nickname = '旧名字';
      dir.lookupFn = (ids) async => [_user('7', nickname)];

      dir.resolve('7');
      await Future<void>.delayed(const Duration(milliseconds: 150));
      expect(dir.resolve('7')?.displayName, '旧名字');

      nickname = '新名字';
      dir.invalidate('7');
      dir.resolve('7');
      await Future<void>.delayed(const Duration(milliseconds: 150));
      expect(dir.resolve('7')?.displayName, '新名字');
    });

    test('fetch 绕过缓存并回填', () async {
      final dir = UserDirectory();
      var nickname = 'v1';
      dir.lookupFn = (ids) async => [_user('5', nickname)];

      dir.resolve('5');
      await Future<void>.delayed(const Duration(milliseconds: 150));
      expect(dir.resolve('5')?.displayName, 'v1');

      nickname = 'v2';
      final fresh = await dir.fetch('5');
      expect(fresh?.displayName, 'v2');
      expect(dir.resolve('5')?.displayName, 'v2');
    });
  });
}
