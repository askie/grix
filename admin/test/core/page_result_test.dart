import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/core/network/api_exception.dart';
import 'package:grix_admin/core/network/page_result.dart';

void main() {
  group('PageResult.fromData', () {
    test('解析标准分页结构', () {
      final data = {
        'items': [
          {'name': 'a'},
          {'name': 'b'},
        ],
        'total': 42,
        'page': 2,
        'page_size': 20,
      };
      final result = PageResult.fromData(
        data,
        (json) => (json['name'] ?? '').toString(),
      );
      expect(result.items, ['a', 'b']);
      expect(result.total, 42);
      expect(result.page, 2);
      expect(result.pageSize, 20);
    });

    test('缺省字段使用安全默认值', () {
      final result = PageResult.fromData(<String, dynamic>{}, (json) => json);
      expect(result.items, isEmpty);
      expect(result.total, 0);
      expect(result.page, 1);
      expect(result.pageSize, 20);
    });

    test('非分页结构抛出可读 API 异常', () {
      expect(
        () => PageResult.fromData('missing backend route', (json) => json),
        throwsA(
          isA<ApiException>().having(
            (e) => e.message,
            'message',
            contains('接口返回格式异常'),
          ),
        ),
      );
    });

    test('列表项不是对象时抛出可读 API 异常', () {
      expect(
        () => PageResult.fromData({
          'items': ['bad item'],
        }, (json) => json),
        throwsA(
          isA<ApiException>().having(
            (e) => e.message,
            'message',
            contains('接口返回列表格式异常'),
          ),
        ),
      );
    });
  });
}
