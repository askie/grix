import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/local/favorite_usage_store.dart';
import 'package:grix/data/providers/user_favorite_path_service.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('FavoriteUsageStore', () {
    setUp(() {
      SharedPreferences.setMockInitialValues({});
    });

    test('returns empty map when no data persisted', () async {
      final store = FavoriteUsageStore();
      expect(await store.load(), isEmpty);
    });

    test('touchAll records current timestamp for ids', () async {
      final store = FavoriteUsageStore();
      final before = DateTime.now().millisecondsSinceEpoch;
      await store.touchAll({'f1', 'f2'});
      final after = DateTime.now().millisecondsSinceEpoch;

      final usage = await store.load();
      expect(usage.keys, unorderedEquals(['f1', 'f2']));
      expect(usage['f1'], greaterThanOrEqualTo(before));
      expect(usage['f1'], lessThanOrEqualTo(after));
      expect(usage['f2'], greaterThanOrEqualTo(before));
      expect(usage['f2'], lessThanOrEqualTo(after));
    });

    test('touchAll updates existing timestamp', () async {
      final store = FavoriteUsageStore();
      await store.touchAll({'f1'});
      final first = (await store.load())['f1'];
      await Future<void>.delayed(const Duration(milliseconds: 5));
      await store.touchAll({'f1'});
      final second = (await store.load())['f1'];

      expect(second, greaterThan(first!));
    });

    test('prune removes stale ids and keeps valid ones', () async {
      final store = FavoriteUsageStore();
      await store.touchAll({'keep', 'remove'});
      await store.prune({'keep'});

      final usage = await store.load();
      expect(usage.containsKey('keep'), isTrue);
      expect(usage.containsKey('remove'), isFalse);
    });

    test('isolates storage per user', () async {
      final storeA = FavoriteUsageStore(userId: 'user_a');
      final storeB = FavoriteUsageStore(userId: 'user_b');

      await storeA.touchAll({'f1'});
      await storeB.touchAll({'f2'});

      final usageA = await storeA.load();
      final usageB = await storeB.load();

      expect(usageA.keys, unorderedEquals(['f1']));
      expect(usageB.keys, unorderedEquals(['f2']));
      expect(usageA.containsKey('f2'), isFalse);
      expect(usageB.containsKey('f1'), isFalse);
    });

    test('fallback key is used when userId is null', () async {
      final store = FavoriteUsageStore();
      await store.touchAll({'f1'});

      final prefs = await SharedPreferences.getInstance();
      expect(
        prefs.getString('favorite_path_last_used_v1'),
        isNotNull,
      );
      expect(
        prefs.getString('favorite_path_last_used_v1:'),
        isNull,
      );
    });
  });

  group('FavoriteUsageStore.sortByLastUsed', () {
    FavoritePathItem item(String id) => FavoritePathItem(
          id: id,
          path: '/$id',
          name: id,
          isDirectory: false,
          machineName: 'm1',
          createdAt: '2024-01-01',
        );

    test('sorts used favorites by timestamp descending', () {
      final favorites = [item('a'), item('b'), item('c')];
      final usage = {
        'a': 3000,
        'b': 1000,
        'c': 2000,
      };

      final sorted = FavoriteUsageStore.sortByLastUsed(favorites, usage);

      expect(sorted.map((e) => e.id).toList(), ['a', 'c', 'b']);
    });

    test('places used items before unrecorded items', () {
      final favorites = [item('a'), item('b'), item('c'), item('d')];
      final usage = {
        'b': 2000,
        'd': 1000,
      };

      final sorted = FavoriteUsageStore.sortByLastUsed(favorites, usage);

      expect(sorted.map((e) => e.id).toList(), ['b', 'd', 'a', 'c']);
    });

    test('keeps unrecorded favorites in original order', () {
      final favorites = [item('x'), item('y'), item('z')];
      final sorted = FavoriteUsageStore.sortByLastUsed(favorites, {});

      expect(sorted.map((e) => e.id).toList(), ['x', 'y', 'z']);
    });

    test('handles empty list', () {
      final sorted = FavoriteUsageStore.sortByLastUsed([], {});
      expect(sorted, isEmpty);
    });
  });
}
