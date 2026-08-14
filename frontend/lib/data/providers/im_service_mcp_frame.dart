part of 'im_service.dart';

/// MCP 帧处理扩展：接收后端 mcp_frame 下行，交给 APP 内置 MCP Server 处理后回帧。
extension ImServiceMcpFrameX on ImService {
  Future<void> _handleMcpFrame(Map<String, dynamic> payload) async {
    final mcpSessionId = (payload['mcp_session_id'] as String?) ?? '';
    final frame = payload['frame'];
    if (mcpSessionId.isEmpty || frame == null || frame is! Map) {
      return;
    }
    final frameMap = Map<String, dynamic>.from(frame);

    // 交给 APP 内置 MCP Server 处理
    final response = await AppMcpServer.handleFrame(frameMap);
    if (response == null) {
      return; // notification 类无需回复
    }

    // 回帧
    _sendPacket({
      'cmd': 'mcp_frame',
      'seq': DateTime.now().millisecondsSinceEpoch,
      'payload': {
        'mcp_session_id': mcpSessionId,
        'frame': response,
      },
    }, requireAuthenticated: true);
  }
}
