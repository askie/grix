/// 统一的 API 异常。
///
/// 承载后端业务错误码与可直接展示给用户的消息。
class ApiException implements Exception {
  ApiException(this.message, {this.code = -1, this.statusCode});

  /// 业务错误码（后端 R.code）。
  final int code;

  /// HTTP 状态码（如有）。
  final int? statusCode;

  /// 可展示的错误消息。
  final String message;

  /// 是否为未授权（需要重新登录）。
  bool get isUnauthorized => statusCode == 401 || code == 10001;

  @override
  String toString() => message;
}
