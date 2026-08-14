/// APP 内置 MCP Server 的 WS Transport 层。
/// 接收后端 mcp_frame 下行 → 解析 JSON-RPC → 派发给 MCP Server → 回帧。
library;

import 'app_tool_registry.dart';

/// 处理从后端来的 mcp_frame 下行帧，执行 MCP 协议逻辑，返回响应帧。
/// 一期用最小 JSON-RPC 子集实现（initialize/tools.list/tools.call），
/// 后续替换为 mcp_dart SDK 完整实现。
class AppMcpServer {
  AppMcpServer._();

  /// 处理一个 MCP JSON-RPC 请求帧，返回响应帧（可能为 null 表示 notification 无需回复）。
  static Future<Map<String, dynamic>?> handleFrame(
    Map<String, dynamic> frame,
  ) async {
    final method = (frame['method'] as String?) ?? '';
    final id = frame['id']; // 可能是 int、String 或 null

    switch (method) {
      case 'initialize':
        return _result(id, {
          'protocolVersion': '2024-11-05',
          'capabilities': {'tools': {}},
          'serverInfo': {'name': 'grix-app-mcp', 'version': '1.0.0'},
        });

      case 'tools/list':
        final tools = AppToolRegistry.tools
            .map((t) => <String, dynamic>{
                  'name': t.name,
                  'description': t.description,
                  'inputSchema': t.inputSchema,
                })
            .toList();
        return _result(id, {'tools': tools});

      case 'tools/call':
        final params = (frame['params'] as Map<String, dynamic>?) ?? {};
        final toolName = (params['name'] as String?) ?? '';
        final arguments =
            (params['arguments'] as Map<String, dynamic>?) ?? {};
        final result = await AppToolRegistry.invoke(toolName, arguments);
        if (result.isError) {
          return _result(id, {
            'content': [
              {'type': 'text', 'text': result.text}
            ],
            'isError': true,
          });
        }
        return _result(id, {
          'content': [
            {'type': 'text', 'text': result.text}
          ],
        });

      // notifications 不需要回复
      case 'notifications/initialized':
        return null;

      default:
        // 任何 notifications/* 通知按 JSON-RPC 规范都不返回响应，
        // 否则会向 agent 回一个无法匹配的 id:null 响应帧。
        if (method.startsWith('notifications/')) return null;
        return _error(id, -32601, 'Method not found: $method');
    }
  }

  static Map<String, dynamic> _result(dynamic id, Map<String, dynamic> result) {
    return {'jsonrpc': '2.0', 'id': id, 'result': result};
  }

  static Map<String, dynamic> _error(dynamic id, int code, String message) {
    return {
      'jsonrpc': '2.0',
      'id': id,
      'error': {'code': code, 'message': message},
    };
  }
}
