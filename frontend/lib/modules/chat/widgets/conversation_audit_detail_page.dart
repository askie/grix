import 'dart:async';
import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:get/get.dart';

import '../../../data/providers/agent_service.dart';
import '../../../data/providers/im_service.dart';
import '../../../shared/utils/toast_util.dart';
import '../../system/agent_client_type_meta.dart';
import 'send_message_to_agent_dialog.dart';

/// Manifest 和时间线随页面打开加载；大段正文仍按用户操作分块读取。
class ConversationAuditDetailPage extends StatefulWidget {
  const ConversationAuditDetailPage({
    super.key,
    required this.sessionId,
    required this.msgId,
  });

  final String sessionId;
  final String msgId;

  @override
  State<ConversationAuditDetailPage> createState() =>
      _ConversationAuditDetailPageState();
}

class _ConversationAuditDetailPageState
    extends State<ConversationAuditDetailPage> {
  static const int _autoContentMaxChunks = 512;
  static const int _autoContentMaxBytes = 64 * 1024 * 1024;

  final ImService _imService = Get.find<ImService>();
  AgentService? get _agentService =>
      Get.isRegistered<AgentService>() ? Get.find<AgentService>() : null;
  Map<String, dynamic>? _manifest;
  String? _errorMessage;
  bool _loadingManifest = true;
  int? _revision;
  List<Map<String, dynamic>> _auditTargets = <Map<String, dynamic>>[];
  String? _selectedAgentId;

  final List<Map<String, dynamic>> _spans = <Map<String, dynamic>>[];
  String? _nextSpanCursor;
  bool _loadingSpans = false;
  bool _spansLoaded = false;
  bool _hasMoreSpans = false;

  final Map<String, _AuditContentView> _contentViews =
      <String, _AuditContentView>{};
  final Map<String, Future<_AuditContentView>> _contentLoadFutures =
      <String, Future<_AuditContentView>>{};
  final Map<String, Set<String>> _contentSeenCursors = <String, Set<String>>{};
  final Map<String, int> _contentLoadedChunkCounts = <String, int>{};
  int _contentLoadGeneration = 0;

  @override
  void initState() {
    super.initState();
    final agentService = _agentService;
    if (agentService != null && !agentService.hasLoaded.value) {
      unawaited(agentService.loadAgents());
    }
    _loadManifest();
  }

  @override
  void dispose() {
    _contentLoadGeneration++;
    _contentLoadFutures.clear();
    super.dispose();
  }

  Future<void> _loadManifest() async {
    setState(() {
      _loadingManifest = true;
      _errorMessage = null;
    });
    try {
      final response = await _imService.requestConversationAuditManifest(
        sessionId: widget.sessionId,
        msgId: widget.msgId,
        agentId: _selectedAgentId,
      );
      if (!mounted) return;
      final error = _responseError(response);
      if (error != null) {
        setState(() => _errorMessage = error);
        return;
      }
      final result = _asMap(response['result']);
      final targets = _asListOfMaps(response['targets']);
      if (_selectedAgentId == null && targets.isNotEmpty) {
        setState(() {
          _auditTargets = targets;
          _manifest = null;
        });
        return;
      }
      final nextRevision =
          _asInt(response['revision']) ?? _asInt(result['revision']);
      setState(() {
        if (_revision != null && nextRevision != _revision) {
          _resetContentLoads();
        }
        _auditTargets = <Map<String, dynamic>>[];
        _manifest = result;
        _revision = nextRevision;
      });
      if (result['has_spans'] == true) {
        await _loadSpans();
      } else {
        setState(() => _spansLoaded = true);
      }
    } catch (error) {
      if (mounted) {
        setState(
          () => _errorMessage = 'chat_audit_detail_manifest_load_failed'
              .trParams({'error': userFacingError(error)}),
        );
      }
    } finally {
      if (mounted) setState(() => _loadingManifest = false);
    }
  }

  Future<void> _loadSpans() async {
    if (_loadingSpans || (_spansLoaded && !_hasMoreSpans)) return;
    setState(() => _loadingSpans = true);
    try {
      final response = await _imService.requestConversationAuditSpans(
        sessionId: widget.sessionId,
        msgId: widget.msgId,
        agentId: _selectedAgentId,
        revision: _revision,
        cursor: _nextSpanCursor,
        limit: 50,
      );
      if (!mounted) return;
      final error = _responseError(response);
      if (error != null) {
        _showError(error);
        return;
      }
      final result = _asMap(response['result']);
      final items = _asListOfMaps(result['items']);
      setState(() {
        _spans.addAll(items);
        _nextSpanCursor = _asNullableString(result['next_cursor']);
        _hasMoreSpans = result['has_more'] == true;
        _spansLoaded = true;
      });
    } catch (error) {
      _showError(
        'chat_audit_detail_spans_load_failed'.trParams({
          'error': userFacingError(error),
        }),
      );
    } finally {
      if (mounted) setState(() => _loadingSpans = false);
    }
  }

  Future<void> _loadContent(String contentId) async {
    if (_loadingManifest) return;
    final generation = _contentLoadGeneration;
    final agentId = _selectedAgentId;
    final revision = _revision;
    try {
      _validateAutoContentLimits(
        contentId,
        _contentViews[contentId] ?? const _AuditContentView(),
      );
      await _loadNextContentChunk(
        contentId: contentId,
        generation: generation,
        agentId: agentId,
        revision: revision,
        maxBytes: 32768,
        maxTotalBytes: _autoContentMaxBytes,
      );
    } on _AuditContentLoadCancelled {
      return;
    } catch (error) {
      _showError(
        'chat_audit_detail_content_load_failed'.trParams({
          'error': userFacingError(error),
        }),
      );
    }
  }

  Future<String> _loadFullContent(
    String contentId,
    bool Function() isCancelled,
  ) async {
    if (_loadingManifest) throw const _AuditContentLoadCancelled();
    final generation = _contentLoadGeneration;
    final agentId = _selectedAgentId;
    final revision = _revision;
    var current = _contentViews[contentId] ?? const _AuditContentView();

    while (!current.eof) {
      if (isCancelled() || !_isContentLoadActive(generation)) {
        throw const _AuditContentLoadCancelled();
      }
      _validateAutoContentLimits(contentId, current);
      current = await _loadNextContentChunk(
        contentId: contentId,
        generation: generation,
        agentId: agentId,
        revision: revision,
        maxBytes: 131072,
        maxTotalBytes: _autoContentMaxBytes,
      );
    }
    if (isCancelled() || !_isContentLoadActive(generation)) {
      throw const _AuditContentLoadCancelled();
    }
    return current.value;
  }

  Future<_AuditContentView> _loadNextContentChunk({
    required String contentId,
    required int generation,
    required String? agentId,
    required int? revision,
    required int maxBytes,
    int? maxTotalBytes,
  }) async {
    if (!_isContentLoadActive(generation)) {
      throw const _AuditContentLoadCancelled();
    }
    final cached = _contentViews[contentId] ?? const _AuditContentView();
    if (cached.eof) return cached;

    final inFlight = _contentLoadFutures[contentId];
    if (inFlight != null) return inFlight;

    final future = _requestNextContentChunk(
      contentId: contentId,
      generation: generation,
      agentId: agentId,
      revision: revision,
      maxBytes: maxBytes,
      maxTotalBytes: maxTotalBytes,
    );
    _contentLoadFutures[contentId] = future;
    try {
      return await future;
    } finally {
      if (identical(_contentLoadFutures[contentId], future)) {
        _contentLoadFutures.remove(contentId);
      }
    }
  }

  Future<_AuditContentView> _requestNextContentChunk({
    required String contentId,
    required int generation,
    required String? agentId,
    required int? revision,
    required int maxBytes,
    int? maxTotalBytes,
  }) async {
    var current = _contentViews[contentId] ?? const _AuditContentView();
    final seenCursors = _contentSeenCursors.putIfAbsent(
      contentId,
      () => <String>{},
    );
    if (current.nextCursor case final cursor?) {
      seenCursors.add(cursor);
    }

    if (_isContentLoadActive(generation)) {
      setState(() {
        _contentViews[contentId] = current.copyWith(loading: true);
      });
    }

    try {
      final response = await _imService.requestConversationAuditContentChunk(
        sessionId: widget.sessionId,
        msgId: widget.msgId,
        contentId: contentId,
        agentId: agentId,
        revision: revision,
        cursor: current.nextCursor,
        maxBytes: maxBytes,
      );
      if (!_isContentLoadActive(generation)) {
        throw const _AuditContentLoadCancelled();
      }
      final error = _responseError(response);
      if (error != null) throw Exception(error);

      final result = _asMap(response['result']);
      final value = result['value']?.toString() ?? '';
      final chunkBytes = utf8.encode(value).length;
      final eof = result['eof'] == true;
      final nextCursor = _asNullableString(result['next_cursor']);
      final byteStart = _asInt(result['byte_start']);
      final byteEnd = _asInt(result['byte_end']);
      final totalBytes = _asInt(result['total_bytes']);
      final offsetFieldCount = [
        byteStart,
        byteEnd,
        totalBytes,
      ].where((value) => value != null).length;
      final offsetsValid =
          offsetFieldCount == 0 ||
          (offsetFieldCount == 3 &&
              byteStart == current.loadedBytes &&
              byteEnd == current.loadedBytes + chunkBytes &&
              totalBytes! >= byteEnd! &&
              (!eof || byteEnd == totalBytes));
      final cursorValid =
          eof ||
          (value.isNotEmpty &&
              nextCursor != null &&
              seenCursors.add(nextCursor));
      if (!offsetsValid || !cursorValid) {
        throw FormatException('chat_audit_detail_response_malformed'.tr);
      }
      if (maxTotalBytes != null &&
          ((totalBytes ?? current.loadedBytes + chunkBytes) > maxTotalBytes)) {
        throw FormatException('chat_audit_detail_response_malformed'.tr);
      }

      current = _AuditContentView(
        value: '${current.value}$value',
        nextCursor: nextCursor,
        eof: eof,
        loading: false,
        loadedBytes: current.loadedBytes + chunkBytes,
        totalBytes: totalBytes ?? current.totalBytes,
      );
      if (_isContentLoadActive(generation)) {
        _contentLoadedChunkCounts[contentId] =
            (_contentLoadedChunkCounts[contentId] ?? 0) + 1;
        setState(() => _contentViews[contentId] = current);
      }
      return current;
    } catch (error) {
      if (error is! _AuditContentLoadCancelled &&
          _isContentLoadActive(generation)) {
        setState(() {
          _contentViews[contentId] = current.copyWith(loading: false);
        });
      }
      rethrow;
    }
  }

  bool _isContentLoadActive(int generation) =>
      mounted && generation == _contentLoadGeneration;

  void _validateAutoContentLimits(String contentId, _AuditContentView current) {
    if ((_contentLoadedChunkCounts[contentId] ?? 0) >= _autoContentMaxChunks ||
        current.loadedBytes > _autoContentMaxBytes ||
        (current.totalBytes ?? 0) > _autoContentMaxBytes) {
      throw FormatException('chat_audit_detail_response_malformed'.tr);
    }
  }

  void _resetContentLoads() {
    _contentLoadGeneration++;
    _contentLoadFutures.clear();
    _contentSeenCursors.clear();
    _contentLoadedChunkCounts.clear();
    _contentViews.clear();
  }

  void _showError(String message) {
    if (!mounted) return;
    CustomToast.show(message, isError: true);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('chat_audit_detail_title'.tr),
        actions: [
          if (_manifest != null)
            Padding(
              padding: const EdgeInsetsDirectional.only(end: 12),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 140),
                child: _StatusPill(status: _manifest!['status'], compact: true),
              ),
            ),
        ],
      ),
      body: _loadingManifest && _manifest == null
          ? const Center(child: CircularProgressIndicator())
          : _errorMessage != null
          ? _ErrorView(message: _errorMessage!, onRetry: _loadManifest)
          : _auditTargets.isNotEmpty && _selectedAgentId == null
          ? _buildTargetSelector(context)
          : _buildResult(context),
    );
  }

  Widget _buildTargetSelector(BuildContext context) {
    final theme = Theme.of(context);
    final agentService = _agentService;
    if (agentService == null) {
      return _buildTargetSelectorList(theme);
    }
    return Obx(
      () => _buildTargetSelectorList(theme, agentService: agentService),
    );
  }

  Widget _buildTargetSelectorList(
    ThemeData theme, {
    AgentService? agentService,
  }) {
    return ListView(
      padding: const EdgeInsets.all(16),
      children: [
        Text(
          'chat_audit_detail_target_selector_hint'.tr,
          style: theme.textTheme.bodyLarge,
        ),
        const SizedBox(height: 12),
        for (final target in _auditTargets)
          Card(
            child: ListTile(
              title: Text(_auditTargetTitle(target, agentService)),
              subtitle: Text(
                '${'chat_audit_detail_target_state'.tr}: ${_auditTargetStateLabel(target['state'])} · '
                '${'chat_audit_detail_target_revision'.tr}: ${target['revision'] ?? 0}',
              ),
              trailing: const Icon(Icons.chevron_right),
              onTap: () => _selectAuditTarget(target['agent_id']),
            ),
          ),
      ],
    );
  }

  String _auditTargetTitle(
    Map<String, dynamic> target,
    AgentService? agentService,
  ) {
    final agentId = _asNullableString(target['agent_id']);
    if (agentId == null) {
      return 'Agent -';
    }
    if (agentService != null) {
      for (final agent in agentService.allAccessibleAgents) {
        if (agent.id.trim() != agentId) {
          continue;
        }
        final agentName = agent.agentName.trim();
        if (agentName.isNotEmpty) {
          return agentName;
        }
        break;
      }
    }
    return 'Agent $agentId';
  }

  void _selectAuditTarget(dynamic agentId) {
    final selected = _asNullableString(agentId);
    if (selected == null) return;
    setState(() {
      _selectedAgentId = selected;
      _auditTargets = <Map<String, dynamic>>[];
      _manifest = null;
      _revision = null;
      _spans.clear();
      _nextSpanCursor = null;
      _spansLoaded = false;
      _hasMoreSpans = false;
      _resetContentLoads();
    });
    _loadManifest();
  }

  Widget _buildResult(BuildContext context) {
    final manifest = _manifest ?? const <String, dynamic>{};
    final statistics = _asMap(manifest['statistics']);
    final usage = _asMap(statistics['total_usage']);
    final input = _asMap(usage['input']);
    final output = _asMap(usage['output']);
    final contentRefs = _asListOfMaps(manifest['content_refs']);
    final durationMs = _turnDurationMs(_spans);
    final cacheRead = _asInt(input['cacheRead']);
    final inputTotal = _asInt(input['total']);
    final cacheHitRate =
        cacheRead != null && inputTotal != null && inputTotal > 0
        ? cacheRead / inputTotal
        : null;

    return RefreshIndicator(
      onRefresh: _refresh,
      child: ListView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(16, 8, 16, 28),
        children: [
          _AuditHero(
            durationMs: durationMs,
            providerText: _auditProviderDisplayValue(manifest),
            modelId: manifest['model_id'],
            startedAt: manifest['started_at'],
            auditId: manifest['audit_id'],
            onAnalyze: _showSendToAgentDialog,
          ),
          const SizedBox(height: 24),
          _SectionTitle('chat_audit_detail_statistics_section'.tr),
          const SizedBox(height: 12),
          _StatisticsGrid(
            items: [
              _StatisticItem(
                icon: Icons.view_in_ar_outlined,
                label: 'chat_audit_detail_stat_model_calls'.tr,
                value: statistics['llm_request_count'],
              ),
              _StatisticItem(
                icon: Icons.build_outlined,
                label: 'chat_audit_detail_stat_tool_calls'.tr,
                value: statistics['tool_call_count'],
              ),
              _StatisticItem(
                icon: Icons.login_outlined,
                label: 'chat_audit_detail_stat_input_token'.tr,
                value: input['total'],
              ),
              _StatisticItem(
                icon: Icons.logout_outlined,
                label: 'chat_audit_detail_stat_output_token'.tr,
                value: output['total'],
              ),
              _StatisticItem(
                icon: Icons.storage_outlined,
                label: 'chat_audit_detail_stat_cache_read'.tr,
                value: input['cacheRead'],
              ),
              _StatisticItem(
                icon: Icons.track_changes_outlined,
                label: 'chat_audit_detail_stat_cache_hit_rate'.tr,
                value: cacheHitRate == null
                    ? null
                    : '${(cacheHitRate * 100).toStringAsFixed(1)}%',
              ),
              _StatisticItem(
                icon: Icons.pie_chart_outline,
                label: 'chat_audit_detail_stat_total_token'.tr,
                value: usage['total_processed'],
              ),
            ],
          ),
          const SizedBox(height: 24),
          _SectionTitle('chat_audit_detail_timeline_section'.tr),
          const SizedBox(height: 12),
          _TokenTimelineChart(spans: _spans, onTap: _showSpanJson),
          const SizedBox(height: 12),
          _TimelineCard(
            spans: _spans,
            loading: _loadingSpans,
            loaded: _spansLoaded,
            hasMore: _hasMoreSpans,
            onLoadMore: _loadSpans,
            onTap: _showSpanJson,
          ),
          const SizedBox(height: 10),
          const _CaptureNotice(),
          const SizedBox(height: 24),
          _QualitySection(quality: _asMap(manifest['quality'])),
          if (contentRefs.isNotEmpty) ...[
            const SizedBox(height: 14),
            _AuditContentSection(
              references: contentRefs,
              views: _contentViews,
              onLoad: _loadContent,
            ),
          ],
        ],
      ),
    );
  }

  Future<void> _refresh() async {
    setState(() {
      _spans.clear();
      _nextSpanCursor = null;
      _spansLoaded = false;
      _hasMoreSpans = false;
      _resetContentLoads();
    });
    await _loadManifest();
  }

  Future<void> _showSpanJson(Map<String, dynamic> span) {
    return showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      useSafeArea: true,
      backgroundColor: Theme.of(context).colorScheme.surface,
      builder: (context) =>
          _SpanJsonSheet(span: span, loadContent: _loadFullContent),
    );
  }

  String _buildAuditPromptTemplate(String auditId) {
    return 'chat_audit_detail_send_prompt_template'.trParams({
      'sessionId': widget.sessionId,
      'auditId': auditId,
    });
  }

  Future<void> _showSendToAgentDialog() async {
    final manifest = _manifest;
    if (manifest == null) {
      return;
    }

    final auditId = _asNullableString(manifest['audit_id'])?.trim() ?? '';
    await showSendMessageToAgentDialog(
      context,
      initialMessage: _buildAuditPromptTemplate(auditId),
      title: 'chat_audit_detail_send_to_ai_title',
      messageLabel: 'chat_audit_detail_send_prompt_label',
      header: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SelectableText(
            'chat_audit_detail_audit_id_label'.trParams({'auditId': auditId}),
          ),
          const SizedBox(height: 4),
          SelectableText(
            'chat_audit_detail_session_id_label'.trParams({
              'sessionId': widget.sessionId,
            }),
          ),
        ],
      ),
    );
  }
}

class _AuditHero extends StatelessWidget {
  const _AuditHero({
    required this.durationMs,
    required this.providerText,
    required this.modelId,
    required this.startedAt,
    required this.auditId,
    required this.onAnalyze,
  });

  final int? durationMs;
  final String providerText;
  final Object? modelId;
  final Object? startedAt;
  final Object? auditId;
  final VoidCallback onAnalyze;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final modelText = _displayValue(modelId);
    final auditIdText = _asNullableString(auditId)?.trim() ?? '';
    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: colors.primary.withValues(alpha: 0.06),
        borderRadius: BorderRadius.circular(18),
        border: Border.all(color: colors.primary.withValues(alpha: 0.18)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(Icons.schedule, color: colors.primary),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  'chat_audit_detail_total_duration'.tr,
                  style: theme.textTheme.titleMedium,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              const SizedBox(width: 8),
              Flexible(
                child: Wrap(
                  alignment: WrapAlignment.end,
                  spacing: 8,
                  runSpacing: 6,
                  children: [
                    if (providerText != '—') _Tag(text: providerText),
                    if (modelText != '—')
                      _Tag(text: modelText, icon: Icons.view_in_ar_outlined),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 10),
          Text(
            _formatDuration(durationMs),
            style: theme.textTheme.displaySmall?.copyWith(
              fontWeight: FontWeight.w700,
              color: colors.onSurface,
            ),
          ),
          const SizedBox(height: 8),
          Row(
            children: [
              Expanded(
                child: Text(
                  '${_formatTime(startedAt)}  ·  ${_compactId(auditId)}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: colors.onSurfaceVariant,
                  ),
                ),
              ),
              if (auditIdText.isNotEmpty) ...[
                const SizedBox(width: 6),
                Tooltip(
                  message: 'chat_audit_id_copy_tooltip'.tr,
                  child: IconButton(
                    visualDensity: VisualDensity.compact,
                    iconSize: 18,
                    onPressed: () async {
                      await Clipboard.setData(ClipboardData(text: auditIdText));
                      if (!context.mounted) return;
                      CustomToast.show(
                        'chat_audit_id_copied'.tr,
                        isError: false,
                      );
                    },
                    icon: const Icon(Icons.copy_rounded),
                  ),
                ),
              ],
              const SizedBox(width: 4),
              Tooltip(
                message: 'chat_audit_detail_send_to_ai_tooltip'.tr,
                child: IconButton(
                  key: const Key('audit_ai_button'),
                  visualDensity: VisualDensity.compact,
                  iconSize: 18,
                  onPressed: onAnalyze,
                  icon: const Icon(Icons.smart_toy_rounded),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _StatisticsGrid extends StatelessWidget {
  const _StatisticsGrid({required this.items});

  final List<_StatisticItem> items;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final width = (constraints.maxWidth - 10) / 2;
        return Wrap(
          spacing: 10,
          runSpacing: 10,
          children: [
            for (final item in items)
              SizedBox(
                width: width,
                child: _StatisticCard(item: item),
              ),
          ],
        );
      },
    );
  }
}

class _StatisticItem {
  const _StatisticItem({
    required this.icon,
    required this.label,
    required this.value,
  });

  final IconData icon;
  final String label;
  final Object? value;
}

class _StatisticCard extends StatelessWidget {
  const _StatisticCard({required this.item});

  final _StatisticItem item;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    return Container(
      constraints: const BoxConstraints(minHeight: 104),
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: colors.surfaceContainerLow,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: colors.outlineVariant.withValues(alpha: 0.7)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(item.icon, size: 20, color: colors.primary),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  item.label,
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: colors.onSurfaceVariant,
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            _displayValue(item.value),
            style: theme.textTheme.headlineSmall?.copyWith(
              fontWeight: FontWeight.w700,
            ),
          ),
        ],
      ),
    );
  }
}

class _TimelineCard extends StatelessWidget {
  const _TimelineCard({
    required this.spans,
    required this.loading,
    required this.loaded,
    required this.hasMore,
    required this.onLoadMore,
    required this.onTap,
  });

  final List<Map<String, dynamic>> spans;
  final bool loading;
  final bool loaded;
  final bool hasMore;
  final VoidCallback onLoadMore;
  final ValueChanged<Map<String, dynamic>> onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    if (loading && spans.isEmpty) {
      return const Padding(
        padding: EdgeInsets.symmetric(vertical: 28),
        child: Center(child: CircularProgressIndicator()),
      );
    }
    if (loaded && spans.isEmpty) {
      return Container(
        padding: const EdgeInsets.all(18),
        decoration: BoxDecoration(
          color: colors.surfaceContainerLow,
          borderRadius: BorderRadius.circular(16),
        ),
        child: Text('chat_audit_detail_no_spans'.tr),
      );
    }
    return Container(
      padding: const EdgeInsets.symmetric(vertical: 8),
      decoration: BoxDecoration(
        color: colors.surfaceContainerLow,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: colors.outlineVariant.withValues(alpha: 0.7)),
      ),
      child: Column(
        children: [
          for (var index = 0; index < spans.length; index++)
            _TimelineTile(
              span: spans[index],
              first: index == 0,
              last: index == spans.length - 1,
              onTap: () => onTap(spans[index]),
            ),
          if (hasMore)
            TextButton.icon(
              onPressed: loading ? null : onLoadMore,
              icon: loading
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.expand_more),
              label: Text('chat_audit_detail_load_more_spans'.tr),
            ),
        ],
      ),
    );
  }
}

class _TokenTimelineChart extends StatefulWidget {
  const _TokenTimelineChart({required this.spans, required this.onTap});

  final List<Map<String, dynamic>> spans;
  final ValueChanged<Map<String, dynamic>> onTap;

  @override
  State<_TokenTimelineChart> createState() => _TokenTimelineChartState();
}

class _TokenTimelineChartState extends State<_TokenTimelineChart> {
  static const double _chartHeight = 184;
  static const double _chartHorizontalInset = 12;
  static const double _minZoomedSlotWidth = 8;
  static const double _maxSlotWidth = 92;
  static const Color _outputBarColor = Color(0xFF22A06B);

  final ScrollController _scrollController = ScrollController();
  bool _showInput = true;
  bool _showOutput = true;
  bool _fitAll = true;
  double _slotWidth = 34;

  @override
  void dispose() {
    _scrollController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (widget.spans.isEmpty) return const SizedBox.shrink();

    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final points = widget.spans
        .map(_TokenTimelinePoint.fromSpan)
        .toList(growable: false);
    final hasTokenData = points.any(
      (point) => point.inputTokens > 0 || point.outputTokens > 0,
    );

    return LayoutBuilder(
      builder: (context, constraints) {
        final viewportWidth = math.max(1.0, constraints.maxWidth - 24);
        final fitSlotWidth = _fitSlotWidth(viewportWidth, points.length);
        final effectiveSlotWidth = _fitAll ? fitSlotWidth : _slotWidth;
        final contentWidth = math.max(
          viewportWidth,
          points.length * effectiveSlotWidth + _chartHorizontalInset * 2,
        );
        final maxVisibleValue = _maxVisibleTokenValue(points);

        return Container(
          padding: const EdgeInsets.fromLTRB(12, 10, 12, 12),
          decoration: BoxDecoration(
            color: colors.surfaceContainerLow,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(
              color: colors.outlineVariant.withValues(alpha: 0.7),
            ),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Wrap(
                      spacing: 8,
                      runSpacing: 6,
                      crossAxisAlignment: WrapCrossAlignment.center,
                      children: [
                        FilterChip(
                          key: const ValueKey('audit-token-chart-input-toggle'),
                          selected: _showInput,
                          showCheckmark: false,
                          avatar: _LegendDot(color: colors.primary),
                          label: Text('chat_audit_detail_timeline_input'.tr),
                          onSelected: (_) => _toggleInput(),
                        ),
                        FilterChip(
                          key: const ValueKey(
                            'audit-token-chart-output-toggle',
                          ),
                          selected: _showOutput,
                          showCheckmark: false,
                          avatar: const _LegendDot(color: _outputBarColor),
                          label: Text('chat_audit_detail_timeline_output'.tr),
                          onSelected: (_) => _toggleOutput(),
                        ),
                      ],
                    ),
                  ),
                  Tooltip(
                    message: 'chat_audit_detail_token_chart_zoom_out'.tr,
                    child: IconButton(
                      visualDensity: VisualDensity.compact,
                      onPressed: () => _zoom(
                        factor: 1 / 1.35,
                        viewportWidth: viewportWidth,
                        fitSlotWidth: fitSlotWidth,
                      ),
                      icon: const Icon(Icons.zoom_out_map_outlined),
                    ),
                  ),
                  Tooltip(
                    message: 'chat_audit_detail_token_chart_zoom_in'.tr,
                    child: IconButton(
                      visualDensity: VisualDensity.compact,
                      onPressed: () => _zoom(
                        factor: 1.35,
                        viewportWidth: viewportWidth,
                        fitSlotWidth: fitSlotWidth,
                      ),
                      icon: const Icon(Icons.zoom_in_map_outlined),
                    ),
                  ),
                  Tooltip(
                    message: 'chat_audit_detail_token_chart_fit_all'.tr,
                    child: IconButton(
                      visualDensity: VisualDensity.compact,
                      onPressed: () => _setFitAll(),
                      icon: const Icon(Icons.fit_screen_outlined),
                    ),
                  ),
                ],
              ),
              const SizedBox(height: 8),
              ClipRRect(
                borderRadius: BorderRadius.circular(12),
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    color: colors.surface.withValues(alpha: 0.72),
                  ),
                  child: Scrollbar(
                    controller: _scrollController,
                    thumbVisibility: !_fitAll && contentWidth > viewportWidth,
                    child: SingleChildScrollView(
                      controller: _scrollController,
                      scrollDirection: Axis.horizontal,
                      physics: const ClampingScrollPhysics(),
                      child: GestureDetector(
                        key: const ValueKey('audit-token-timeline-chart'),
                        behavior: HitTestBehavior.opaque,
                        onTapUp: (details) => _handleChartTap(
                          details.localPosition.dx,
                          effectiveSlotWidth,
                          points,
                        ),
                        child: SizedBox(
                          width: contentWidth,
                          height: _chartHeight,
                          child: CustomPaint(
                            painter: _TokenTimelinePainter(
                              points: points,
                              showInput: _showInput,
                              showOutput: _showOutput,
                              inputColor: colors.primary,
                              outputColor: _outputBarColor,
                              axisColor: colors.outlineVariant,
                              labelColor: colors.onSurfaceVariant,
                              slotWidth: effectiveSlotWidth,
                              horizontalInset: _chartHorizontalInset,
                              maxVisibleValue: maxVisibleValue,
                              scrollController: _scrollController,
                              viewportWidth: viewportWidth,
                              textScaler: MediaQuery.textScalerOf(context),
                              textDirection: Directionality.of(context),
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
              if (!hasTokenData) ...[
                const SizedBox(height: 8),
                Text(
                  'chat_audit_detail_token_chart_no_usage'.tr,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: colors.onSurfaceVariant,
                  ),
                ),
              ],
            ],
          ),
        );
      },
    );
  }

  void _toggleInput() {
    if (_showInput && !_showOutput) return;
    setState(() => _showInput = !_showInput);
  }

  void _toggleOutput() {
    if (_showOutput && !_showInput) return;
    setState(() => _showOutput = !_showOutput);
  }

  double _fitSlotWidth(double viewportWidth, int pointCount) {
    if (pointCount <= 0) return _slotWidth;
    return math.max(
      1,
      (viewportWidth - _chartHorizontalInset * 2) / pointCount,
    );
  }

  int _maxVisibleTokenValue(List<_TokenTimelinePoint> points) {
    var maxValue = 0;
    for (final point in points) {
      if (_showInput) maxValue = math.max(maxValue, point.inputTokens);
      if (_showOutput) maxValue = math.max(maxValue, point.outputTokens);
    }
    return maxValue;
  }

  void _setFitAll() {
    setState(() => _fitAll = true);
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollController.hasClients) return;
      _scrollController.jumpTo(0);
    });
  }

  void _zoom({
    required double factor,
    required double viewportWidth,
    required double fitSlotWidth,
  }) {
    final oldSlotWidth = _fitAll ? fitSlotWidth : _slotWidth;
    final scrollOffset = _scrollController.hasClients
        ? _scrollController.offset
        : 0.0;
    final centerIndex =
        (scrollOffset + viewportWidth / 2 - _chartHorizontalInset) /
        oldSlotWidth;
    final minSlotWidth = math.min(_minZoomedSlotWidth, fitSlotWidth);
    final maxSlotWidth = math.max(_maxSlotWidth, fitSlotWidth);
    final nextSlotWidth = (oldSlotWidth * factor)
        .clamp(minSlotWidth, maxSlotWidth)
        .toDouble();

    setState(() {
      _fitAll = false;
      _slotWidth = nextSlotWidth;
    });

    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!_scrollController.hasClients) return;
      final targetOffset =
          centerIndex * nextSlotWidth +
          _chartHorizontalInset -
          viewportWidth / 2;
      _scrollController.jumpTo(
        targetOffset.clamp(0.0, _scrollController.position.maxScrollExtent),
      );
    });
  }

  void _handleChartTap(
    double localX,
    double slotWidth,
    List<_TokenTimelinePoint> points,
  ) {
    final index = ((localX - _chartHorizontalInset) / slotWidth).floor();
    if (index < 0 || index >= points.length) return;
    widget.onTap(points[index].span);
  }
}

class _LegendDot extends StatelessWidget {
  const _LegendDot({required this.color});

  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: 10,
      height: 10,
      decoration: BoxDecoration(color: color, shape: BoxShape.circle),
    );
  }
}

class _TokenTimelinePoint {
  const _TokenTimelinePoint({
    required this.span,
    required this.inputTokens,
    required this.outputTokens,
  });

  factory _TokenTimelinePoint.fromSpan(Map<String, dynamic> span) {
    final usage = _asMap(span['usage']);
    final input = _asMap(usage['input']);
    final output = _asMap(usage['output']);
    return _TokenTimelinePoint(
      span: span,
      inputTokens: math.max(0, _asInt(input['total']) ?? 0),
      outputTokens: math.max(0, _asInt(output['total']) ?? 0),
    );
  }

  final Map<String, dynamic> span;
  final int inputTokens;
  final int outputTokens;
}

class _TokenTimelinePainter extends CustomPainter {
  const _TokenTimelinePainter({
    required this.points,
    required this.showInput,
    required this.showOutput,
    required this.inputColor,
    required this.outputColor,
    required this.axisColor,
    required this.labelColor,
    required this.slotWidth,
    required this.horizontalInset,
    required this.maxVisibleValue,
    required this.scrollController,
    required this.viewportWidth,
    required this.textScaler,
    required this.textDirection,
  }) : super(repaint: scrollController);

  final List<_TokenTimelinePoint> points;
  final bool showInput;
  final bool showOutput;
  final Color inputColor;
  final Color outputColor;
  final Color axisColor;
  final Color labelColor;
  final double slotWidth;
  final double horizontalInset;
  final int maxVisibleValue;
  final ScrollController scrollController;
  final double viewportWidth;
  final TextScaler textScaler;
  final TextDirection textDirection;

  @override
  void paint(Canvas canvas, Size size) {
    if (points.isEmpty) return;

    const topInset = 14.0;
    const bottomInset = 30.0;
    final plotHeight = math.max(1.0, size.height - topInset - bottomInset);
    final baseline = topInset + plotHeight;
    final maxValue = math.max(1, maxVisibleValue);
    final gridPaint = Paint()
      ..color = axisColor.withValues(alpha: 0.65)
      ..strokeWidth = 1;
    final axisPaint = Paint()
      ..color = axisColor
      ..strokeWidth = 1.2;

    for (var i = 0; i <= 3; i++) {
      final y = topInset + plotHeight * i / 3;
      canvas.drawLine(Offset(0, y), Offset(size.width, y), gridPaint);
    }
    canvas.drawLine(
      Offset(0, baseline),
      Offset(size.width, baseline),
      axisPaint,
    );

    _drawAxisLabel(
      canvas,
      _formatCompactNumber(maxValue),
      const Offset(6, topInset),
    );
    _drawAxisLabel(
      canvas,
      '#1',
      Offset(horizontalInset, baseline + 8),
      alignRight: false,
    );
    _drawAxisLabel(
      canvas,
      '#${points.length}',
      Offset(size.width - horizontalInset, baseline + 8),
      alignRight: true,
    );

    final visibleSeriesCount = math.max(
      1,
      (showInput ? 1 : 0) + (showOutput ? 1 : 0),
    );
    final availableSlotWidth = math.max(0.8, slotWidth * 0.78);
    final barGap = visibleSeriesCount == 2
        ? math.min(1.0, availableSlotWidth * 0.15)
        : 0.0;
    final barWidth = ((availableSlotWidth - barGap) / visibleSeriesCount)
        .clamp(0.4, 14.0)
        .toDouble();
    final scrollOffset = scrollController.hasClients
        ? scrollController.offset
        : 0.0;
    final firstVisibleIndex = ((scrollOffset - horizontalInset) / slotWidth)
        .floor()
        .clamp(0, points.length - 1);
    final lastVisibleIndex =
        ((scrollOffset + viewportWidth - horizontalInset) / slotWidth)
            .ceil()
            .clamp(0, points.length - 1);

    for (var index = firstVisibleIndex; index <= lastVisibleIndex; index++) {
      final point = points[index];
      final centerX = horizontalInset + index * slotWidth + slotWidth / 2;
      final values = <(int, Color)>[
        if (showInput) (point.inputTokens, inputColor),
        if (showOutput) (point.outputTokens, outputColor),
      ];
      final groupWidth =
          values.length * barWidth + math.max(0, values.length - 1) * barGap;
      var x = centerX - groupWidth / 2;
      for (final (value, color) in values) {
        final paint = Paint()..color = color.withValues(alpha: 0.82);
        if (value <= 0) {
          canvas.drawRRect(
            RRect.fromRectAndRadius(
              Rect.fromLTWH(x, baseline - 2, barWidth, 2),
              const Radius.circular(1),
            ),
            paint..color = color.withValues(alpha: 0.35),
          );
        } else {
          final height = math.max(2.0, plotHeight * value / maxValue);
          canvas.drawRRect(
            RRect.fromRectAndRadius(
              Rect.fromLTWH(x, baseline - height, barWidth, height),
              const Radius.circular(2),
            ),
            paint,
          );
        }
        x += barWidth + barGap;
      }
    }
  }

  void _drawAxisLabel(
    Canvas canvas,
    String text,
    Offset offset, {
    bool alignRight = false,
  }) {
    final painter = TextPainter(
      text: TextSpan(
        text: text,
        style: TextStyle(color: labelColor, fontSize: 11),
      ),
      textDirection: textDirection,
      textScaler: textScaler,
      maxLines: 1,
    )..layout();
    final dx = alignRight ? offset.dx - painter.width : offset.dx;
    painter.paint(canvas, Offset(dx, offset.dy));
  }

  @override
  bool shouldRepaint(covariant _TokenTimelinePainter oldDelegate) =>
      points != oldDelegate.points ||
      showInput != oldDelegate.showInput ||
      showOutput != oldDelegate.showOutput ||
      inputColor != oldDelegate.inputColor ||
      outputColor != oldDelegate.outputColor ||
      axisColor != oldDelegate.axisColor ||
      labelColor != oldDelegate.labelColor ||
      slotWidth != oldDelegate.slotWidth ||
      horizontalInset != oldDelegate.horizontalInset ||
      maxVisibleValue != oldDelegate.maxVisibleValue ||
      scrollController != oldDelegate.scrollController ||
      viewportWidth != oldDelegate.viewportWidth ||
      textScaler != oldDelegate.textScaler ||
      textDirection != oldDelegate.textDirection;
}

class _TimelineTile extends StatelessWidget {
  const _TimelineTile({
    required this.span,
    required this.first,
    required this.last,
    required this.onTap,
  });

  final Map<String, dynamic> span;
  final bool first;
  final bool last;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final kind = span['kind']?.toString() ?? '';
    final usage = _asMap(span['usage']);
    final input = _asMap(usage['input']);
    final output = _asMap(usage['output']);
    final detail = <String>[
      if (input['total'] != null)
        '${input['total']} ${'chat_audit_detail_timeline_input'.tr}',
      if (output['total'] != null)
        '${output['total']} ${'chat_audit_detail_timeline_output'.tr}',
    ].join(' → ');
    final durationLabel = _timelineDurationLabel(span);
    return InkWell(
      borderRadius: BorderRadius.circular(14),
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12),
        child: Stack(
          children: [
            Positioned(
              left: 0,
              top: 0,
              bottom: 0,
              width: 28,
              child: CustomPaint(
                painter: _TimelinePainter(
                  color: colors.primary,
                  first: first,
                  last: last,
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.only(left: 36),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.center,
                children: [
                  Container(
                    width: 40,
                    height: 40,
                    margin: const EdgeInsets.symmetric(vertical: 14),
                    decoration: BoxDecoration(
                      color: colors.primary.withValues(alpha: 0.1),
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Icon(
                      _spanIcon(kind),
                      color: colors.primary,
                      size: 21,
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        mainAxisAlignment: MainAxisAlignment.center,
                        children: [
                          Text(
                            span['name']?.toString() ?? kind,
                            style: theme.textTheme.titleSmall?.copyWith(
                              fontWeight: FontWeight.w600,
                            ),
                          ),
                          if (detail.isNotEmpty) ...[
                            const SizedBox(height: 3),
                            Text(
                              detail,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: colors.onSurfaceVariant,
                              ),
                            ),
                          ],
                          const SizedBox(height: 6),
                          Wrap(
                            spacing: 6,
                            runSpacing: 6,
                            crossAxisAlignment: WrapCrossAlignment.center,
                            children: [
                              if (kind.isNotEmpty) _Tag(text: kind),
                              _StatusPill(
                                status: span['status'],
                                compact: true,
                              ),
                              Text(
                                durationLabel,
                                style: theme.textTheme.bodySmall?.copyWith(
                                  color: colors.onSurfaceVariant,
                                ),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                  ),
                  const SizedBox(width: 4),
                  Icon(Icons.chevron_right, color: colors.onSurfaceVariant),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _TimelinePainter extends CustomPainter {
  const _TimelinePainter({
    required this.color,
    required this.first,
    required this.last,
  });

  final Color color;
  final bool first;
  final bool last;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color.withValues(alpha: 0.55)
      ..strokeWidth = 1.5;
    final center = Offset(size.width / 2, size.height / 2);
    if (!first) canvas.drawLine(Offset(center.dx, 0), center, paint);
    if (!last) canvas.drawLine(center, Offset(center.dx, size.height), paint);
    canvas.drawCircle(center, 5, Paint()..color = color);
    canvas.drawCircle(
      center,
      8,
      Paint()
        ..color = color.withValues(alpha: 0.16)
        ..style = PaintingStyle.fill,
    );
  }

  @override
  bool shouldRepaint(covariant _TimelinePainter oldDelegate) =>
      color != oldDelegate.color ||
      first != oldDelegate.first ||
      last != oldDelegate.last;
}

class _SpanJsonSheet extends StatefulWidget {
  const _SpanJsonSheet({required this.span, required this.loadContent});

  final Map<String, dynamic> span;
  final Future<String> Function(String contentId, bool Function() isCancelled)
  loadContent;

  @override
  State<_SpanJsonSheet> createState() => _SpanJsonSheetState();
}

class _SpanJsonSheetState extends State<_SpanJsonSheet> {
  final Map<String, String> _loadedContent = <String, String>{};
  final Map<String, String> _contentErrors = <String, String>{};
  late final Set<String> _contentIds;
  late Map<String, dynamic> _displaySpan;
  late bool _loadingContent;

  @override
  void initState() {
    super.initState();
    _contentIds = _collectMissingContentIds(widget.span);
    _displaySpan = _injectLoadedContent(widget.span, _loadedContent);
    _loadingContent = _contentIds.isNotEmpty;
    if (_loadingContent) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) unawaited(_performContentLoad(_contentIds));
      });
    }
  }

  Future<void> _performContentLoad(Iterable<String> contentIds) async {
    final targets = contentIds.toList(growable: false);
    final loaded = <String, String>{};
    final errors = <String, String>{};
    await Future.wait([
      for (final contentId in targets)
        () async {
          try {
            loaded[contentId] = await widget.loadContent(
              contentId,
              () => !mounted,
            );
          } on _AuditContentLoadCancelled {
            return;
          } catch (error) {
            errors[contentId] = '$error';
          }
        }(),
    ]);
    if (!mounted) return;
    setState(() {
      _loadedContent.addAll(loaded);
      for (final contentId in targets) {
        _contentErrors.remove(contentId);
      }
      _contentErrors.addAll(errors);
      _displaySpan = _injectLoadedContent(widget.span, _loadedContent);
      _loadingContent = false;
    });
  }

  void _retryFailedContent() {
    if (_loadingContent || _contentErrors.isEmpty) return;
    final targets = _contentErrors.keys.toList(growable: false);
    setState(() {
      _loadingContent = true;
      _contentErrors.clear();
    });
    unawaited(_performContentLoad(targets));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final colors = theme.colorScheme;
    final json = const JsonEncoder.withIndent('  ').convert(_displaySpan);
    final title =
        widget.span['name']?.toString() ??
        'chat_audit_detail_span_fallback_title'.tr;
    final kind = widget.span['kind']?.toString() ?? '';
    final jsonStyle = theme.textTheme.bodySmall?.copyWith(
      color: colors.onInverseSurface,
      fontFamily: 'monospace',
      height: 1.5,
    );
    return FractionallySizedBox(
      heightFactor: 0.82,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        'chat_audit_detail_span_sheet_title'.tr,
                        style: theme.textTheme.titleLarge,
                      ),
                      const SizedBox(height: 3),
                      Text(
                        '$title${kind.isEmpty ? '' : ' · $kind'}',
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: colors.onSurfaceVariant,
                        ),
                      ),
                    ],
                  ),
                ),
                IconButton(
                  onPressed: () => Navigator.of(context).pop(),
                  icon: const Icon(Icons.close),
                ),
              ],
            ),
            const SizedBox(height: 12),
            Expanded(
              child: SingleChildScrollView(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      width: double.infinity,
                      padding: const EdgeInsets.all(14),
                      decoration: BoxDecoration(
                        color: colors.inverseSurface,
                        borderRadius: BorderRadius.circular(14),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          if (_loadingContent) ...[
                            const LinearProgressIndicator(
                              key: ValueKey('audit-span-content-loading'),
                            ),
                            const SizedBox(height: 10),
                          ],
                          if (_contentErrors.isNotEmpty) ...[
                            Row(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Icon(
                                  Icons.error_outline,
                                  size: 18,
                                  color: colors.error,
                                ),
                                const SizedBox(width: 8),
                                Expanded(
                                  child: Text(
                                    'chat_audit_detail_content_load_failed'
                                        .trParams({
                                          'error': _contentErrors.entries
                                              .map(
                                                (entry) =>
                                                    '${entry.key}: ${entry.value}',
                                              )
                                              .join('; '),
                                        }),
                                    maxLines: 3,
                                    overflow: TextOverflow.ellipsis,
                                    style: theme.textTheme.bodySmall?.copyWith(
                                      color: colors.onInverseSurface,
                                    ),
                                  ),
                                ),
                                TextButton(
                                  onPressed: _retryFailedContent,
                                  child: Text('common_retry'.tr),
                                ),
                              ],
                            ),
                            const SizedBox(height: 8),
                          ],
                          SelectionArea(
                            key: const ValueKey('audit-span-json'),
                            child: _JsonTreeView(
                              value: _displaySpan,
                              style: jsonStyle,
                            ),
                          ),
                        ],
                      ),
                    ),
                    const SizedBox(height: 10),
                    Text(
                      'chat_audit_detail_span_sheet_note'.tr,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: colors.onSurfaceVariant,
                      ),
                    ),
                  ],
                ),
              ),
            ),
            const SizedBox(height: 12),
            SizedBox(
              width: double.infinity,
              child: FilledButton.icon(
                onPressed: _loadingContent || _contentErrors.isNotEmpty
                    ? null
                    : () async {
                        await Clipboard.setData(ClipboardData(text: json));
                        if (!context.mounted) return;
                        CustomToast.show(
                          'chat_audit_detail_json_copied'.tr,
                          isError: false,
                        );
                      },
                icon: const Icon(Icons.copy_all_outlined),
                label: Text('chat_audit_detail_copy_full_json'.tr),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _JsonTreeView extends StatelessWidget {
  const _JsonTreeView({required this.value, required this.style});

  final Object? value;
  final TextStyle? style;

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: _buildJsonRows(value, depth: 0, path: r'$', style: style),
    );
  }
}

class _CollapsibleJsonContentLine extends StatefulWidget {
  const _CollapsibleJsonContentLine({
    required this.value,
    required this.depth,
    required this.path,
    required this.trailingComma,
    required this.style,
  });

  final String value;
  final int depth;
  final String path;
  final bool trailingComma;
  final TextStyle? style;

  @override
  State<_CollapsibleJsonContentLine> createState() =>
      _CollapsibleJsonContentLineState();
}

class _CollapsibleJsonContentLineState
    extends State<_CollapsibleJsonContentLine> {
  static const int _collapsedMaxLines = 8;

  bool _expanded = false;

  @override
  Widget build(BuildContext context) {
    final text =
        '${_jsonIndent(widget.depth)}"content": '
        '${jsonEncode(widget.value)}${widget.trailingComma ? ',' : ''}';
    return LayoutBuilder(
      builder: (context, constraints) {
        final effectiveStyle = DefaultTextStyle.of(
          context,
        ).style.merge(widget.style);
        final collapsedPainter = TextPainter(
          text: TextSpan(text: text, style: effectiveStyle),
          maxLines: _collapsedMaxLines,
          textDirection: Directionality.of(context),
          textScaler: MediaQuery.textScalerOf(context),
        )..layout(maxWidth: constraints.maxWidth);
        final exceedsCollapsedLines = collapsedPainter.didExceedMaxLines;
        collapsedPainter.dispose();

        final expandedPainter = TextPainter(
          text: TextSpan(text: text, style: effectiveStyle),
          textDirection: Directionality.of(context),
          textScaler: MediaQuery.textScalerOf(context),
        )..layout(maxWidth: constraints.maxWidth);
        final horizontallyOverflows = expandedPainter.computeLineMetrics().any(
          (line) => line.width > constraints.maxWidth + 0.5,
        );
        expandedPainter.dispose();

        final canToggle = exceedsCollapsedLines || horizontallyOverflows;
        final textWidget = Text(
          text,
          key: ValueKey('audit-span-content-value:${widget.path}'),
          maxLines: canToggle && !_expanded ? _collapsedMaxLines : null,
          overflow: canToggle && !_expanded
              ? TextOverflow.ellipsis
              : TextOverflow.clip,
          softWrap: true,
          style: widget.style,
        );
        final displayedText = _expanded && horizontallyOverflows
            ? SingleChildScrollView(
                key: ValueKey(
                  'audit-span-content-horizontal-scroll:${widget.path}',
                ),
                scrollDirection: Axis.horizontal,
                child: Text(
                  text,
                  key: ValueKey('audit-span-content-value:${widget.path}'),
                  softWrap: false,
                  overflow: TextOverflow.clip,
                  style: widget.style,
                ),
              )
            : textWidget;

        return Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            displayedText,
            if (canToggle)
              Padding(
                padding: EdgeInsets.only(left: widget.depth * 12),
                child: TextButton.icon(
                  key: ValueKey('audit-span-content-toggle:${widget.path}'),
                  onPressed: () => setState(() => _expanded = !_expanded),
                  icon: Icon(
                    _expanded ? Icons.unfold_less : Icons.unfold_more,
                    size: 18,
                  ),
                  label: Text(
                    _expanded ? 'common_collapse'.tr : 'common_expand'.tr,
                  ),
                ),
              ),
          ],
        );
      },
    );
  }
}

class _QualitySection extends StatelessWidget {
  const _QualitySection({required this.quality});

  final Map<String, dynamic> quality;

  @override
  Widget build(BuildContext context) {
    final entries = <(String, Object?)>[
      ('chat_audit_detail_quality_input'.tr, quality['input_complete']),
      ('chat_audit_detail_quality_output'.tr, quality['output_complete']),
      ('chat_audit_detail_quality_usage'.tr, quality['usage_complete']),
      (
        'chat_audit_detail_quality_tool_calls'.tr,
        quality['tool_calls_complete'],
      ),
      ('chat_audit_detail_quality_subagents'.tr, quality['subagents_complete']),
      (
        'chat_audit_detail_quality_raw_requests'.tr,
        quality['raw_requests_complete'],
      ),
    ];
    final complete = entries
        .where((entry) => entry.$2 != null)
        .every((entry) => _qualityCollected(entry.$2));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(
              child: _SectionTitle('chat_audit_detail_quality_section'.tr),
            ),
            _StatusPill(status: complete ? 'completed' : 'partial'),
          ],
        ),
        const SizedBox(height: 12),
        Container(
          decoration: BoxDecoration(
            color: Theme.of(context).colorScheme.surfaceContainerLow,
            borderRadius: BorderRadius.circular(16),
            border: Border.all(
              color: Theme.of(
                context,
              ).colorScheme.outlineVariant.withValues(alpha: 0.7),
            ),
          ),
          child: Column(
            children: [
              for (var index = 0; index < entries.length; index++)
                _QualityRow(
                  label: entries[index].$1,
                  value: entries[index].$2,
                  divider: index != entries.length - 1,
                ),
            ],
          ),
        ),
      ],
    );
  }
}

class _QualityRow extends StatelessWidget {
  const _QualityRow({
    required this.label,
    required this.value,
    required this.divider,
  });

  final String label;
  final Object? value;
  final bool divider;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final collected = _qualityCollected(value);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 13),
      decoration: BoxDecoration(
        border: divider
            ? Border(bottom: BorderSide(color: colors.outlineVariant))
            : null,
      ),
      child: Row(
        children: [
          Icon(
            collected ? Icons.check_circle : Icons.info_outline,
            color: collected ? colors.primary : colors.onSurfaceVariant,
            size: 20,
          ),
          const SizedBox(width: 12),
          Expanded(child: Text(label)),
          Text(
            collected
                ? 'chat_audit_detail_quality_collected'.tr
                : 'chat_audit_detail_quality_disabled'.tr,
            style: TextStyle(
              color: collected ? colors.primary : colors.onSurfaceVariant,
            ),
          ),
        ],
      ),
    );
  }
}

class _AuditContentSection extends StatelessWidget {
  const _AuditContentSection({
    required this.references,
    required this.views,
    required this.onLoad,
  });

  final List<Map<String, dynamic>> references;
  final Map<String, _AuditContentView> views;
  final ValueChanged<String> onLoad;

  @override
  Widget build(BuildContext context) {
    return ExpansionTile(
      tilePadding: const EdgeInsets.symmetric(horizontal: 4),
      title: Text('chat_audit_detail_content_section'.tr),
      subtitle: Text('chat_audit_detail_content_section_subtitle'.tr),
      children: [
        for (final ref in references)
          _ContentReferenceTile(
            reference: ref,
            view:
                views[ref['content_id']?.toString()] ??
                const _AuditContentView(),
            onLoad: onLoad,
          ),
      ],
    );
  }
}

class _ContentReferenceTile extends StatelessWidget {
  const _ContentReferenceTile({
    required this.reference,
    required this.view,
    required this.onLoad,
  });

  final Map<String, dynamic> reference;
  final _AuditContentView view;
  final ValueChanged<String> onLoad;

  @override
  Widget build(BuildContext context) {
    final contentId = reference['content_id']?.toString() ?? '';
    if (contentId.isEmpty) return const SizedBox.shrink();
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              '${reference['kind'] ?? 'chat_audit_detail_content_fallback'.tr} · '
              '${reference['bytes'] ?? 0} ${'chat_audit_detail_bytes'.tr}',
            ),
            if (view.value.isNotEmpty) ...[
              const SizedBox(height: 8),
              SelectableText(view.value),
            ],
            const SizedBox(height: 6),
            if (view.loading)
              const LinearProgressIndicator()
            else if (!view.eof)
              TextButton.icon(
                onPressed: () => onLoad(contentId),
                icon: const Icon(Icons.download_outlined),
                label: Text(
                  view.value.isEmpty
                      ? 'chat_audit_detail_load_text'.tr
                      : 'chat_audit_detail_load_next_text'.tr,
                ),
              )
            else
              Text(
                'chat_audit_detail_content_loaded'.tr,
                style: Theme.of(context).textTheme.bodySmall,
              ),
          ],
        ),
      ),
    );
  }
}

class _CaptureNotice extends StatelessWidget {
  const _CaptureNotice();

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: colors.primary.withValues(alpha: 0.05),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: colors.primary.withValues(alpha: 0.15)),
      ),
      child: Row(
        children: [
          Icon(Icons.info_outline, size: 18, color: colors.primary),
          const SizedBox(width: 8),
          Expanded(child: Text('chat_audit_detail_capture_notice'.tr)),
        ],
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text);

  final String text;

  @override
  Widget build(BuildContext context) => Text(
    text,
    style: Theme.of(
      context,
    ).textTheme.titleLarge?.copyWith(fontWeight: FontWeight.w700),
  );
}

class _Tag extends StatelessWidget {
  const _Tag({required this.text, this.icon});

  final String text;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
      decoration: BoxDecoration(
        color: colors.primary.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: colors.primary.withValues(alpha: 0.18)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 14, color: colors.primary),
            const SizedBox(width: 4),
          ],
          Flexible(
            child: Text(
              text,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: Theme.of(
                context,
              ).textTheme.labelMedium?.copyWith(color: colors.primary),
            ),
          ),
        ],
      ),
    );
  }
}

class _StatusPill extends StatelessWidget {
  const _StatusPill({required this.status, this.compact = false});

  final Object? status;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final normalized = status?.toString().toLowerCase() ?? '';
    final completed =
        normalized == 'completed' ||
        normalized == 'complete' ||
        normalized == 'ok';
    final failed = normalized == 'failed' || normalized == 'cancelled';
    final color = failed
        ? colors.error
        : completed
        ? colors.primary
        : colors.tertiary;
    final label = failed
        ? 'chat_audit_detail_status_failed'.tr
        : completed
        ? 'chat_audit_detail_status_completed'.tr
        : normalized == 'partial'
        ? 'chat_audit_detail_status_partial'.tr
        : 'chat_audit_detail_status_running'.tr;
    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: compact ? 7 : 9,
        vertical: compact ? 3 : 5,
      ),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.1),
        borderRadius: BorderRadius.circular(9),
        border: Border.all(color: color.withValues(alpha: 0.22)),
      ),
      child: Text(
        label,
        overflow: TextOverflow.ellipsis,
        style: Theme.of(context).textTheme.labelMedium?.copyWith(color: color),
      ),
    );
  }
}

class _AuditContentView {
  const _AuditContentView({
    this.value = '',
    this.nextCursor,
    this.eof = false,
    this.loading = false,
    this.loadedBytes = 0,
    this.totalBytes,
  });

  final String value;
  final String? nextCursor;
  final bool eof;
  final bool loading;
  final int loadedBytes;
  final int? totalBytes;

  _AuditContentView copyWith({
    String? value,
    String? nextCursor,
    bool? eof,
    bool? loading,
    int? loadedBytes,
    int? totalBytes,
  }) {
    return _AuditContentView(
      value: value ?? this.value,
      nextCursor: nextCursor ?? this.nextCursor,
      eof: eof ?? this.eof,
      loading: loading ?? this.loading,
      loadedBytes: loadedBytes ?? this.loadedBytes,
      totalBytes: totalBytes ?? this.totalBytes,
    );
  }
}

class _AuditContentLoadCancelled implements Exception {
  const _AuditContentLoadCancelled();
}

class _ErrorView extends StatelessWidget {
  const _ErrorView({required this.message, required this.onRetry});

  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(message, textAlign: TextAlign.center),
            const SizedBox(height: 12),
            OutlinedButton(onPressed: onRetry, child: Text('common_retry'.tr)),
          ],
        ),
      ),
    );
  }
}

IconData _spanIcon(String kind) => switch (kind) {
  'llm_request' => Icons.auto_awesome_outlined,
  'tool_call' => Icons.build_outlined,
  'subagent' => Icons.account_tree_outlined,
  'turn' => Icons.person_outline,
  _ => Icons.bolt_outlined,
};

int? _turnDurationMs(List<Map<String, dynamic>> spans) {
  for (final span in spans) {
    if (span['kind'] == 'turn') return _asInt(span['duration_ms']);
  }
  return null;
}

bool _qualityCollected(Object? value) {
  if (value == null) return false;
  if (value is bool) return value;
  final normalized = value.toString().toLowerCase();
  return normalized == 'complete' ||
      normalized == 'completed' ||
      normalized == 'captured' ||
      normalized == 'true';
}

String _formatDuration(int? durationMs) {
  if (durationMs == null) return '—';
  if (durationMs < 1000) return '${durationMs}ms';
  return '${(durationMs / 1000).toStringAsFixed(2)}s';
}

String _formatCompactNumber(int value) {
  if (value >= 1000000000) return '${(value / 1000000000).toStringAsFixed(1)}B';
  if (value >= 1000000) return '${(value / 1000000).toStringAsFixed(1)}M';
  if (value >= 1000) return '${(value / 1000).toStringAsFixed(1)}K';
  return value.toString();
}

String _auditProviderDisplayValue(Map<String, dynamic> manifest) {
  final provider = _asNullableString(manifest['provider']);
  final explicitClientLabel = _auditClientLabelFromCandidates([
    manifest['agent_client_type'],
    manifest['agentClientType'],
    manifest['client_type'],
    manifest['clientType'],
    manifest['provider_key'],
    manifest['providerKey'],
    manifest['adapter_id'],
    manifest['adapterId'],
  ]);
  final providerClientLabel = _auditClientLabelFromCandidates([provider]);
  final bridgeProvider = _isAuditBridgeProvider(provider);

  if (bridgeProvider && explicitClientLabel != null) {
    return explicitClientLabel;
  }
  if (!bridgeProvider && providerClientLabel != null) {
    return providerClientLabel;
  }
  if (bridgeProvider) {
    final inferred = _auditClientLabelFromModelId(manifest['model_id']);
    if (inferred != null) return inferred;
  }
  return _displayValue(provider);
}

String? _auditClientLabelFromCandidates(Iterable<Object?> values) {
  for (final value in values) {
    final label = _auditClientLabelFromRaw(value);
    if (label != null) return label;
  }
  return null;
}

String? _auditClientLabelFromRaw(Object? value) {
  final normalized = _normalizeAuditClientKey(value);
  if (normalized == null || _isAuditBridgeProvider(normalized)) return null;
  final meta = systemAgentClientTypeMeta(normalized);
  if (meta != null) return meta.label;
  return switch (normalized) {
    'deepseek' => 'DeepSeek',
    _ => null,
  };
}

String? _auditClientLabelFromModelId(Object? value) {
  final model = _asNullableString(value)?.toLowerCase();
  if (model == null) return null;
  if (model.contains('kimi') || model.contains('moonshot')) return 'Kimi';
  if (model.contains('gemini')) return 'Gemini';
  if (model.contains('qwen') || model.contains('qwq')) return 'Qwen';
  if (model.contains('claude')) return 'Claude';
  if (model.contains('deepseek')) return 'DeepSeek';
  if (model.contains('copilot')) return 'GitHub Copilot';
  return null;
}

String? _normalizeAuditClientKey(Object? value) {
  final raw = _asNullableString(value)?.toLowerCase();
  if (raw == null) return null;
  final firstSegment = raw.split('/').first.trim();
  final normalized = firstSegment.replaceAll(RegExp(r'[-_\s]+'), '');
  return switch (normalized) {
    'githubcopilot' => 'copilot',
    'opencode' || 'opencodebase' => 'opencode',
    'codewhale' => 'codewhale',
    'openclaw' => 'openclaw',
    'antigravity' => 'agy',
    'moonshot' => 'kimi',
    _ => normalized,
  };
}

bool _isAuditBridgeProvider(Object? value) {
  final normalized = _normalizeAuditClientKey(value);
  return normalized == 'acp' || normalized == 'agentapi' || normalized == 'api';
}

String _timelineDurationLabel(Map<String, dynamic> span) {
  final durationMs = _asInt(span['duration_ms']);
  final kind = span['kind']?.toString();
  if (durationMs == null) {
    if (kind == 'llm_request') {
      return 'chat_audit_detail_llm_duration_unavailable'.tr;
    }
    return 'chat_audit_detail_duration_missing'.tr;
  }
  if (durationMs == 0 &&
      kind == 'tool_call' &&
      _asNullableString(span['started_at']) ==
          _asNullableString(span['ended_at'])) {
    return 'chat_audit_detail_duration_imprecise'.tr;
  }
  return _formatDuration(durationMs);
}

String _auditTargetStateLabel(Object? value) {
  final normalized = _asNullableString(value)?.toLowerCase();
  return switch (normalized) {
    'accepted' => 'chat_audit_detail_target_state_accepted'.tr,
    'recording' => 'chat_audit_detail_target_state_recording'.tr,
    'finalizing' => 'chat_audit_detail_target_state_finalizing'.tr,
    'ready' => 'chat_audit_detail_target_state_ready'.tr,
    'partial' => 'chat_audit_detail_target_state_partial'.tr,
    'failed' || 'cancelled' => 'chat_audit_detail_target_state_failed'.tr,
    null => 'chat_audit_detail_unknown'.tr,
    _ => value.toString(),
  };
}

String _formatTime(Object? value) {
  final parsed = DateTime.tryParse(value?.toString() ?? '');
  if (parsed == null) return 'chat_audit_detail_time_missing'.tr;
  final local = parsed.toLocal();
  String two(int number) => number.toString().padLeft(2, '0');
  return '${two(local.hour)}:${two(local.minute)}:${two(local.second)}';
}

String _compactId(Object? value) {
  final text = value?.toString() ?? '';
  if (text.length <= 20) return text.isEmpty ? '—' : text;
  return '${text.substring(0, 10)}…${text.substring(text.length - 6)}';
}

String _displayValue(Object? value) {
  if (value == null || value.toString().isEmpty) return '—';
  if (value is num) {
    return value.toString().replaceAllMapped(
      RegExp(r'\B(?=(\d{3})+(?!\d))'),
      (_) => ',',
    );
  }
  return value.toString();
}

Set<String> _collectMissingContentIds(Object? value) {
  final result = <String>{};

  void visit(Object? node) {
    if (node is Map) {
      final contentId = _asNullableString(node['content_id']);
      final hasContent = node.containsKey('content') && node['content'] != null;
      if (contentId != null && !hasContent) result.add(contentId);
      for (final child in node.values) {
        visit(child);
      }
    } else if (node is List) {
      for (final child in node) {
        visit(child);
      }
    }
  }

  visit(value);
  return result;
}

Map<String, dynamic> _injectLoadedContent(
  Map<String, dynamic> span,
  Map<String, String> loadedContent,
) {
  dynamic inject(Object? node) {
    if (node is List) {
      return [for (final child in node) inject(child)];
    }
    if (node is! Map) return node;

    final contentId = _asNullableString(node['content_id']);
    final hasLoadedContent =
        contentId != null && loadedContent.containsKey(contentId);
    final hasCapturedContent = node.containsKey('content');
    final result = <String, dynamic>{};
    for (final entry in node.entries) {
      final key = entry.key.toString();
      if (key == 'content' && contentId != null) continue;
      result[key] = inject(entry.value);
      if (key == 'content_id' && contentId != null) {
        if (hasLoadedContent) {
          result['content'] = loadedContent[contentId];
        } else if (hasCapturedContent) {
          result['content'] = inject(node['content']);
        }
      }
    }
    return result;
  }

  return Map<String, dynamic>.from(inject(span) as Map);
}

List<Widget> _buildJsonRows(
  Object? value, {
  required int depth,
  required String path,
  required TextStyle? style,
  String? fieldName,
  bool trailingComma = false,
}) {
  final prefix = fieldName == null ? '' : '${jsonEncode(fieldName)}: ';
  final comma = trailingComma ? ',' : '';
  final indent = _jsonIndent(depth);

  if (fieldName == 'content' && value is String) {
    return [
      _CollapsibleJsonContentLine(
        value: value,
        depth: depth,
        path: path,
        trailingComma: trailingComma,
        style: style,
      ),
    ];
  }

  if (value is Map) {
    final entries = value.entries.toList(growable: false);
    return [
      Text('$indent$prefix{', style: style),
      for (var index = 0; index < entries.length; index++)
        ..._buildJsonRows(
          entries[index].value,
          depth: depth + 1,
          path: '$path.${entries[index].key}',
          style: style,
          fieldName: entries[index].key.toString(),
          trailingComma: index != entries.length - 1,
        ),
      Text('$indent}$comma', style: style),
    ];
  }

  if (value is List) {
    return [
      Text('$indent$prefix[', style: style),
      for (var index = 0; index < value.length; index++)
        ..._buildJsonRows(
          value[index],
          depth: depth + 1,
          path: '$path[$index]',
          style: style,
          trailingComma: index != value.length - 1,
        ),
      Text('$indent]$comma', style: style),
    ];
  }

  return [Text('$indent$prefix${jsonEncode(value)}$comma', style: style)];
}

String _jsonIndent(int depth) => List.filled(depth, '  ').join();

Map<String, dynamic> _asMap(dynamic value) =>
    value is Map ? Map<String, dynamic>.from(value) : <String, dynamic>{};

List<Map<String, dynamic>> _asListOfMaps(dynamic value) => value is List
    ? value
          .whereType<Map>()
          .map((item) => Map<String, dynamic>.from(item))
          .toList()
    : <Map<String, dynamic>>[];

int? _asInt(dynamic value) =>
    value is num ? value.toInt() : int.tryParse('$value');

String? _asNullableString(dynamic value) {
  final normalized = value?.toString().trim() ?? '';
  return normalized.isEmpty || normalized == 'null' ? null : normalized;
}

String? _responseError(Map<String, dynamic> response) {
  final code = _asNullableString(response['error_code']);
  if (code == null) return null;
  return '$code: '
      '${_asNullableString(response['error_message']) ?? 'chat_audit_detail_request_failed'.tr}';
}
