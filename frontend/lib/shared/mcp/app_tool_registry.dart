/// APP 内置 MCP Server 的能力注册表。
/// 新增能力只需在 [tools] 列表追加一项。
library;

import 'package:get/get.dart';

import 'app_page_navigator.dart';

/// 单个工具的定义与执行回调。
class McpToolDef {
  const McpToolDef({
    required this.name,
    required this.description,
    required this.inputSchema,
    required this.handler,
  });

  final String name;
  final String description;
  final Map<String, dynamic> inputSchema;
  final Future<McpToolResult> Function(Map<String, dynamic> args) handler;
}

/// 工具执行结果。
class McpToolResult {
  const McpToolResult({required this.isError, required this.text});
  factory McpToolResult.success(String text) =>
      McpToolResult(isError: false, text: text);
  factory McpToolResult.error(String text) =>
      McpToolResult(isError: true, text: text);

  final bool isError;
  final String text;
}

/// APP 能力注册表——新增能力只改这里。
class AppToolRegistry {
  AppToolRegistry._();

  /// 每次列出工具时按当前语言生成描述，避免 static 初始化时冻死文案。
  static List<McpToolDef> get tools => [
    McpToolDef(
      name: 'grix_local_search',
      description: 'mcp_tool_local_search_desc'.tr,
      inputSchema: {
        'type': 'object',
        'properties': {
          'keywords': {
            'type': 'array',
            'items': {'type': 'string'},
            'minItems': 1,
            'description': 'mcp_tool_local_search_keywords_desc'.tr,
          },
        },
        'required': ['keywords'],
      },
      handler: _localSearch,
    ),
    McpToolDef(
      name: 'grix_open_chat',
      description: 'mcp_tool_open_chat_desc'.tr,
      inputSchema: {
        'type': 'object',
        'properties': {
          'session_id': {
            'type': 'string',
            'description': 'mcp_tool_open_chat_session_id_desc'.tr,
          },
        },
        'required': ['session_id'],
      },
      handler: _openChat,
    ),
    McpToolDef(
      name: 'grix_open_page',
      description: 'mcp_tool_open_page_desc'.trParams({
        'pages': AppPageNavigator.pagesHint,
      }),
      inputSchema: {
        'type': 'object',
        'properties': {
          'page': {
            'type': 'string',
            'enum': AppPageNavigator.supportedPages,
            'description': 'mcp_tool_open_page_page_desc'.tr,
          },
        },
        'required': ['page'],
      },
      handler: _openPage,
    ),
    // 二期新增能力在此追加
  ];

  static McpToolDef? findTool(String name) {
    final n = name.trim();
    for (final t in tools) {
      if (t.name == n) return t;
    }
    return null;
  }

  static Future<McpToolResult> invoke(
    String name,
    Map<String, dynamic> args,
  ) async {
    final tool = findTool(name);
    if (tool == null) {
      return McpToolResult.error('unknown tool: $name');
    }
    try {
      return await tool.handler(args);
    } catch (e) {
      return McpToolResult.error('execution_failed: $e');
    }
  }

  // --- 工具实现 ---

  static Future<McpToolResult> _localSearch(Map<String, dynamic> args) async {
    final raw = args['keywords'];
    final keywords = (raw is List)
        ? raw.map((e) => '$e').where((s) => s.trim().isNotEmpty).toList()
        : <String>[];
    if (keywords.isEmpty) {
      return McpToolResult.error('keywords required');
    }
    // 打开本地搜索结果页并带入关键词，由页面真正展示结果给用户。
    final ok = AppPageNavigator.openLocalSearch(keywords);
    return ok
        ? McpToolResult.success(
            'mcp_opened_local_search'.trParams({
              'keywords': keywords.join(' '),
            }),
          )
        : McpToolResult.error('open local search failed');
  }

  static Future<McpToolResult> _openChat(Map<String, dynamic> args) async {
    final sessionId = (args['session_id'] as String?)?.trim() ?? '';
    if (sessionId.isEmpty) {
      return McpToolResult.error('session_id required');
    }
    final ok = AppPageNavigator.openChat(sessionId);
    return ok
        ? McpToolResult.success('mcp_opened_chat'.tr)
        : McpToolResult.error('open chat failed');
  }

  static Future<McpToolResult> _openPage(Map<String, dynamic> args) async {
    final page = (args['page'] as String?)?.trim() ?? '';
    if (page.isEmpty) {
      return McpToolResult.error('page required');
    }
    final label = AppPageNavigator.openPage(page);
    if (label == null) {
      return McpToolResult.error('unknown page: $page');
    }
    return McpToolResult.success('mcp_opened_page'.trParams({'page': label}));
  }
}
