import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:dio/dio.dart';

import '../../data/providers/auth_service.dart';
import '../../shared/utils/app_runtime_endpoints.dart';
import '../../shared/utils/time_formatter.dart';

/// 通话历史列表页
class CallHistoryPage extends StatefulWidget {
  const CallHistoryPage({super.key});

  @override
  State<CallHistoryPage> createState() => _CallHistoryPageState();
}

class _CallHistoryPageState extends State<CallHistoryPage> {
  final _items = <Map<String, dynamic>>[];
  bool _loading = false;
  bool _hasMore = true;
  int _page = 1;
  static const _pageSize = 20;

  @override
  void initState() {
    super.initState();
    _loadMore();
  }

  Future<void> _loadMore() async {
    if (_loading || !_hasMore) return;
    setState(() => _loading = true);
    try {
      final auth = Get.find<AuthService>();
      final token = auth.token ?? '';
      final dio = Dio();
      final resp = await dio.get(
        '${AppRuntimeEndpoints.apiBaseUrl}/v1/call-records',
        queryParameters: {'page': _page, 'page_size': _pageSize},
        options: Options(headers: {'Authorization': 'Bearer $token'}),
      );
      final data = resp.data['data'] as Map<String, dynamic>?;
      final newItems = (data?['items'] as List?)?.cast<Map<String, dynamic>>() ?? [];
      setState(() {
        _items.addAll(newItems);
        _hasMore = newItems.length >= _pageSize;
        _page++;
      });
    } catch (_) {
      // 静默失败，保留已有数据
    } finally {
      setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: Text('call_history'.tr)),
      body: RefreshIndicator(
        onRefresh: () async {
          setState(() { _items.clear(); _page = 1; _hasMore = true; });
          await _loadMore();
        },
        child: _items.isEmpty && !_loading
            ? Center(child: Text('call_history_empty'.tr))
            : ListView.builder(
                itemCount: _items.length + (_hasMore ? 1 : 0),
                itemBuilder: (ctx, i) {
                  if (i == _items.length) {
                    if (!_loading) _loadMore();
                    return const Center(
                      child: Padding(
                        padding: EdgeInsets.all(16),
                        child: CircularProgressIndicator(),
                      ),
                    );
                  }
                  return _CallRecordTile(record: _items[i]);
                },
              ),
      ),
    );
  }
}

class _CallRecordTile extends StatelessWidget {
  final Map<String, dynamic> record;
  const _CallRecordTile({required this.record});

  @override
  Widget build(BuildContext context) {
    final state = record['state'] as int? ?? 0;
    final duration = record['duration_seconds'] as int?;
    final startedAt = record['started_at'] as int?;

    final stateLabel = _stateLabel(state);
    final stateColor = _stateColor(state);
    final durationText = duration != null ? _formatDuration(duration) : '';
    final timeText = startedAt != null
        ? TimeFormatter.formatChatTime(startedAt)
        : '';

    return ListTile(
      leading: Icon(Icons.call, color: stateColor),
      title: Text('${record['caller_id']} → ${record['callee_id']}'),
      subtitle: Text('$timeText  $durationText'),
      trailing: Text(stateLabel, style: TextStyle(color: stateColor, fontSize: 12)),
    );
  }

  String _stateLabel(int state) {
    switch (state) {
      case 0: return 'call_state_ringing'.tr;
      case 1: return 'call_state_active'.tr;
      case 2: return 'call_state_ended'.tr;
      case 3: return 'call_state_rejected'.tr;
      case 4: return 'call_state_missed'.tr;
      default: return 'call_state_error'.tr;
    }
  }

  Color _stateColor(int state) {
    switch (state) {
      case 2: return Colors.green;
      case 3: return Colors.orange;
      case 4: return Colors.red;
      default: return Colors.grey;
    }
  }

  String _formatDuration(int seconds) {
    final m = seconds ~/ 60;
    final s = seconds % 60;
    return '${m.toString().padLeft(2, '0')}:${s.toString().padLeft(2, '0')}';
  }
}
