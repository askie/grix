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

  static final List<McpToolDef> tools = [
    McpToolDef(
      name: 'grix_local_search',
      description:
          '在用户本机聊天记录中按关键词搜索会话与消息，并打开搜索结果页展示给用户。\n'
          '【核心用法】把用户的自然语言搜索意图，拆解并扩展成多个「可能的关键词」放入 keywords 数组。\n'
          '【关键词为 OR 关系】各关键词之间是「或」——命中任意一个即返回，所以应尽量给出同义词、'
          '别名、相关说法来提高召回。例：用户说「找我和老王聊装修的那些记录」，可给 '
          '["老王","装修","房子","施工","报价"]；用户说「上次发的那个会议链接」，可给 '
          '["会议","腾讯会议","zoom","meeting","链接"]。\n'
          '【匹配方式】不区分大小写的子串模糊匹配（被搜文本只要包含该关键词即命中）。'
          '不支持引号短语、通配符 *、正则、AND、字段限定等任何高级语法——keywords 只是一组朴素关键词。\n'
          '【搜索范围】会话的标题/对方昵称/用户名/最后一条消息，以及消息正文。\n'
          '【关键词选取】用实词（人名、昵称、话题、地点、专有名词等），去掉「的、了、那个、记录」这类虚词。',
      inputSchema: {
        'type': 'object',
        'properties': {
          'keywords': {
            'type': 'array',
            'items': {'type': 'string'},
            'minItems': 1,
            'description':
                '可能的关键词列表，彼此为 OR 关系（命中任一即返回）。'
                '把用户意图扩展成多个同义/相关词以提高召回；每个元素是一个独立关键词，做子串模糊匹配。',
          },
        },
        'required': ['keywords'],
      },
      handler: _localSearch,
    ),
    McpToolDef(
      name: 'grix_open_chat',
      description: '打开指定会话的聊天页面，让用户直接进入与某个联系人或群的对话。',
      inputSchema: {
        'type': 'object',
        'properties': {
          'session_id': {'type': 'string', 'description': '要打开的会话 ID'},
        },
        'required': ['session_id'],
      },
      handler: _openChat,
    ),
    McpToolDef(
      name: 'grix_open_page',
      description:
          '打开 APP 内的指定页面，帮助用户快速跳转到某个功能或设置页。可用页面（名称(用途)）：'
          '${AppPageNavigator.pagesHint}。',
      inputSchema: {
        'type': 'object',
        'properties': {
          'page': {
            'type': 'string',
            'enum': AppPageNavigator.supportedPages,
            'description': '目标页面名',
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
