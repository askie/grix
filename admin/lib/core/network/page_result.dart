import 'api_exception.dart';

/// 通用分页结果。
///
/// 对应后端列表接口统一返回的 `{items, total, page, page_size}`。
class PageResult<T> {
  PageResult({
    required this.items,
    required this.total,
    required this.page,
    required this.pageSize,
  });

  final List<T> items;
  final int total;
  final int page;
  final int pageSize;

  /// 从信封 data 解析。[parse] 负责把单条 Map 转为 T。
  factory PageResult.fromData(
    dynamic data,
    T Function(Map<String, dynamic> json) parse,
  ) {
    if (data is! Map) {
      throw ApiException('接口返回格式异常，请确认后端服务已更新');
    }
    final map = data.cast<String, dynamic>();
    final raw = (map['items'] as List?) ?? const [];
    return PageResult<T>(
      items: raw
          .map((e) {
            if (e is! Map) {
              throw ApiException('接口返回列表格式异常，请确认后端服务已更新');
            }
            return parse(e.cast<String, dynamic>());
          })
          .toList(growable: false),
      total: (map['total'] as num?)?.toInt() ?? 0,
      page: (map['page'] as num?)?.toInt() ?? 1,
      pageSize: (map['page_size'] as num?)?.toInt() ?? 20,
    );
  }

  static PageResult<T> empty<T>() =>
      PageResult<T>(items: const [], total: 0, page: 1, pageSize: 20);
}
