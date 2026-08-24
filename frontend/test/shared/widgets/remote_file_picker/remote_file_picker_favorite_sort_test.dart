import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/local/favorite_usage_store.dart';
import 'package:grix/data/providers/user_favorite_path_service.dart';

void main() {
  group('RemoteFilePicker favorite sort', () {
    FavoritePathItem item(String id, {String machine = 'm1'}) =>
        FavoritePathItem(
          id: id,
          path: '/$id',
          name: id,
          isDirectory: false,
          machineName: machine,
          createdAt: '2024-01-01',
        );

    test('filters by machine then sorts by last used', () {
      final all = [
        item('a', machine: 'm1'),
        item('b', machine: 'm2'),
        item('c', machine: 'm1'),
        item('d', machine: 'm1'),
      ];
      final visible = all.where((f) => f.machineName == 'm1').toList();
      final usage = {
        'd': 5000,
        'a': 8000,
      };

      final sorted = FavoriteUsageStore.sortByLastUsed(visible, usage);

      expect(sorted.map((e) => e.id).toList(), ['a', 'd', 'c']);
    });

    test('unrecorded items keep server order within the filtered list', () {
      final all = [
        item('x', machine: 'm1'),
        item('y', machine: 'm1'),
        item('z', machine: 'm1'),
      ];
      final usage = <String, int>{};

      final sorted = FavoriteUsageStore.sortByLastUsed(all, usage);

      expect(sorted.map((e) => e.id).toList(), ['x', 'y', 'z']);
    });
  });
}
