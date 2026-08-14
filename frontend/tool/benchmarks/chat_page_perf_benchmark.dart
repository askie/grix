import 'dart:convert';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/models/message_model.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/data/providers/oss_service.dart';
import 'package:grix/data/providers/session_service.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/modules/chat/controllers/chat_controller.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';
import 'package:grix/shared/widgets/chat_markdown_ast_view.dart';
import 'package:grix/shared/widgets/message_bubble.dart';
import 'package:get/get.dart';

const bool _benchEnabled = bool.fromEnvironment(
  'CHAT_BENCH_ENABLE',
  defaultValue: false,
);
const int _messageCountDefine = int.fromEnvironment(
  'CHAT_BENCH_MESSAGE_COUNT',
  defaultValue: 5000,
);
const int _messageLengthDefine = int.fromEnvironment(
  'CHAT_BENCH_MESSAGE_LENGTH',
  defaultValue: 280,
);
const int _iterationsDefine = int.fromEnvironment(
  'CHAT_BENCH_ITERATIONS',
  defaultValue: 5,
);
const int _parseSampleCountDefine = int.fromEnvironment(
  'CHAT_BENCH_PARSE_SAMPLE_COUNT',
  defaultValue: 2000,
);
const String _markdownRatioDefine = String.fromEnvironment(
  'CHAT_BENCH_MARKDOWN_RATIO',
  defaultValue: '0.45',
);
const String _loadModeDefine = String.fromEnvironment(
  'CHAT_BENCH_LOAD_MODE',
  defaultValue: 'prefilled_full',
);
const String _chatTypeDefine = String.fromEnvironment(
  'CHAT_BENCH_CHAT_TYPE',
  defaultValue: 'private',
);
const int _senderPoolSizeDefine = int.fromEnvironment(
  'CHAT_BENCH_SENDER_POOL_SIZE',
  defaultValue: 2,
);
const int _mentionItemsDefine = int.fromEnvironment(
  'CHAT_BENCH_MENTION_ITEMS',
  defaultValue: 0,
);
const int _trimProbeAppendCountDefine = int.fromEnvironment(
  'CHAT_BENCH_TRIM_PROBE_APPEND',
  defaultValue: 0,
);
const int _simulatedInitialWindowLimit = 60;
const int _simulatedMessagePageSize = 20;
const int _simulatedResidentMessageCap = 100;

enum _BenchLoadMode {
  prefilledFull,
  realisticWindow;

  static _BenchLoadMode parse(String raw) {
    switch (raw.trim().toLowerCase()) {
      case 'realistic_window':
      case 'real':
      case 'window':
        return _BenchLoadMode.realisticWindow;
      default:
        return _BenchLoadMode.prefilledFull;
    }
  }

  String get wireValue {
    switch (this) {
      case _BenchLoadMode.prefilledFull:
        return 'prefilled_full';
      case _BenchLoadMode.realisticWindow:
        return 'realistic_window';
    }
  }
}

class _BenchPrefilledImService extends ImService {
  @override
  void enterSession(
    String sessionId, {
    Duration initialLoadDelay = Duration.zero,
  }) {}

  @override
  void leaveSession([String? explicitSessionId]) {}

  @override
  void connect(String wsUrl) {}
}

class _BenchAuthService extends AuthService {
  @override
  bool get isLoggedIn => true;

  @override
  String? get userId => '1001';
}

class _BenchAgentService extends AgentService {
  @override
  Future<void> loadAgents({String? categoryId}) async {}
}

class _BenchSessionService extends SessionService {
  _BenchSessionService(this.config);

  final _BenchConfig config;

  @override
  Future<SessionDetailResult> fetchSessionDetailResult(String sessionId) async {
    if (config.chatType == 'group') {
      final memberCount = math.max(10, config.senderPoolSize);
      final members = List<Map<String, dynamic>>.generate(memberCount, (index) {
        final id = '${2000 + index}';
        return <String, dynamic>{
          'member_id': id,
          'member_type': 1,
          'nickname': 'member_$id',
        };
      });
      return SessionDetailResult(
        data: {
          'session_type': 2,
          'member_count': memberCount,
          'members': members,
        },
      );
    }
    return const SessionDetailResult(
      data: {
        'session_type': 1,
        'member_count': 0,
        'members': <Map<String, dynamic>>[],
      },
    );
  }
}

class _BenchOssService extends OssService {}

class _BenchConfig {
  const _BenchConfig({
    required this.enabled,
    required this.messageCount,
    required this.messageLength,
    required this.iterations,
    required this.parseSampleCount,
    required this.markdownRatio,
    required this.loadMode,
    required this.trimProbeAppendCount,
    required this.chatType,
    required this.senderPoolSize,
    required this.mentionItems,
  });

  final bool enabled;
  final int messageCount;
  final int messageLength;
  final int iterations;
  final int parseSampleCount;
  final double markdownRatio;
  final _BenchLoadMode loadMode;
  final int trimProbeAppendCount;
  final String chatType;
  final int senderPoolSize;
  final int mentionItems;

  static _BenchConfig fromEnvironment() {
    const safeMessageCount = _messageCountDefine <= 0
        ? 5000
        : _messageCountDefine;
    const safeMessageLength = _messageLengthDefine <= 0
        ? 280
        : _messageLengthDefine;
    const safeIterations = _iterationsDefine <= 0 ? 5 : _iterationsDefine;
    const safeParseSampleCount = _parseSampleCountDefine <= 0
        ? 2000
        : _parseSampleCountDefine;
    const safeTrimProbeAppendCount = _trimProbeAppendCountDefine <= 0
        ? 0
        : _trimProbeAppendCountDefine;
    final normalizedChatType = _chatTypeDefine.trim().toLowerCase() == 'group'
        ? 'group'
        : 'private';
    const safeSenderPoolSize = _senderPoolSizeDefine <= 0
        ? 2
        : _senderPoolSizeDefine;
    const safeMentionItems = _mentionItemsDefine <= 0 ? 0 : _mentionItemsDefine;
    final parsedMarkdownRatio = double.tryParse(_markdownRatioDefine) ?? 0.45;
    final safeMarkdownRatio = parsedMarkdownRatio.clamp(0.0, 1.0);
    final loadMode = _BenchLoadMode.parse(_loadModeDefine);

    return _BenchConfig(
      enabled: _benchEnabled,
      messageCount: safeMessageCount,
      messageLength: safeMessageLength,
      iterations: safeIterations,
      parseSampleCount: safeParseSampleCount,
      markdownRatio: safeMarkdownRatio,
      loadMode: loadMode,
      trimProbeAppendCount: safeTrimProbeAppendCount,
      chatType: normalizedChatType,
      senderPoolSize: safeSenderPoolSize,
      mentionItems: safeMentionItems,
    );
  }
}

class _ChatUiSample {
  const _ChatUiSample({
    required this.iteration,
    required this.openFirstFrameMs,
    required this.openSettleMs,
    required this.scrollSettleMs,
    required this.keyboardActivateSettleMs,
    required this.visibleBubbleCount,
    required this.visibleAstCount,
    required this.visibleSelectableTextCount,
    required this.loadedMessageCount,
    required this.hasOlderMessages,
    required this.visibleMsgIndexMin,
    required this.visibleMsgIndexMax,
    required this.postTrimMessageCount,
    required this.trimProbeElapsedMs,
  });

  final int iteration;
  final int openFirstFrameMs;
  final int openSettleMs;
  final int scrollSettleMs;
  final int keyboardActivateSettleMs;
  final int visibleBubbleCount;
  final int visibleAstCount;
  final int visibleSelectableTextCount;
  final int loadedMessageCount;
  final bool hasOlderMessages;
  final int visibleMsgIndexMin;
  final int visibleMsgIndexMax;
  final int postTrimMessageCount;
  final int trimProbeElapsedMs;

  Map<String, dynamic> toJson() {
    return {
      'iteration': iteration,
      'open_first_frame_ms': openFirstFrameMs,
      'open_settle_ms': openSettleMs,
      'scroll_settle_ms': scrollSettleMs,
      'keyboard_activate_settle_ms': keyboardActivateSettleMs,
      'visible_bubble_count': visibleBubbleCount,
      'visible_ast_count': visibleAstCount,
      'visible_selectable_text_count': visibleSelectableTextCount,
      'loaded_message_count': loadedMessageCount,
      'has_older_messages': hasOlderMessages,
      'visible_msg_index_min': visibleMsgIndexMin,
      'visible_msg_index_max': visibleMsgIndexMax,
      'post_trim_message_count': postTrimMessageCount,
      'trim_probe_elapsed_ms': trimProbeElapsedMs,
    };
  }
}

Future<int> _measureKeyboardActivateSettleMs(
  WidgetTester tester,
  ChatController controller,
) async {
  final inputFinder = find.byType(TextField).last;
  if (inputFinder.evaluate().isEmpty) {
    return 0;
  }
  final keyboardStopwatch = Stopwatch()..start();
  await tester.tap(inputFinder);
  await tester.pump();
  final hadClientsBeforeInset = controller.scrollController.hasClients;
  const keyboardInsetSteps = <double>[80, 140, 200, 260, 300];
  for (final inset in keyboardInsetSteps) {
    tester.view.viewInsets = FakeViewPadding(bottom: inset);
    await tester.pump(const Duration(milliseconds: 16));
  }
  await tester.pumpAndSettle();
  keyboardStopwatch.stop();
  if (hadClientsBeforeInset && controller.scrollController.hasClients) {
    tester.view.viewInsets = FakeViewPadding.zero;
    await tester.pumpAndSettle();
  }
  return keyboardStopwatch.elapsedMilliseconds;
}

String _repeatToken(String token, int count) {
  if (count <= 0 || token.isEmpty) {
    return '';
  }
  final buffer = StringBuffer();
  for (var i = 0; i < count; i++) {
    buffer.write(token);
  }
  return buffer.toString();
}

String _ensureLength(String seed, int minLength) {
  if (seed.length >= minLength) {
    return seed;
  }
  final padLength = minLength - seed.length;
  final padding = _repeatToken(' lorem', (padLength / 6).ceil());
  return '$seed$padding';
}

String _buildPlainContent(int index, int targetLength) {
  final seed = 'plain_message_$index ${_repeatToken('token', 8)}';
  return _ensureLength(seed, targetLength);
}

String _buildMarkdownContent(int index, int targetLength) {
  final seed =
      '''# Benchmark $index

- item a
- item b
- item c

```json
{"id": $index, "ok": true, "name": "bench_$index"}
```

| key | value |
| --- | --- |
| id | $index |
| mode | benchmark |

https://example.com/bench/$index
''';
  return _ensureLength(seed, targetLength);
}

List<MessageModel> _buildMessages({
  required int count,
  required int length,
  required double markdownRatio,
  required String sessionId,
  required int senderPoolSize,
}) {
  final markdownPercent = (markdownRatio * 100).round();
  final poolSize = math.max(2, senderPoolSize);
  return List<MessageModel>.generate(count, (index) {
    final useMarkdown = (index % 100) < markdownPercent;
    final senderId = index.isEven ? '1001' : '${2000 + (index % poolSize)}';
    return MessageModel(
      msgId: 'bench_${sessionId}_msg_$index',
      sessionId: sessionId,
      senderId: senderId,
      content: useMarkdown
          ? _buildMarkdownContent(index, length)
          : _buildPlainContent(index, length),
      createdAt: index + 1,
    );
  }, growable: false);
}

int _extractBenchMessageIndex(String msgId) {
  final match = RegExp(r'(\d+)$').firstMatch(msgId);
  if (match == null) {
    return -1;
  }
  return int.tryParse(match.group(1) ?? '') ?? -1;
}

Future<void> _prepareMessagesForIteration(
  _BenchConfig config,
  List<MessageModel> messages,
) async {
  final imService = Get.find<ImService>();
  if (config.loadMode == _BenchLoadMode.prefilledFull) {
    imService.currentMessages.assignAll(messages);
    return;
  }

  final keep = math.min(_simulatedInitialWindowLimit, messages.length);
  final start = messages.length - keep;
  imService.currentMessages.assignAll(messages.sublist(start, messages.length));
}

Future<Map<String, int>> _runTrimProbe(
  _BenchConfig config,
  List<MessageModel> allMessages,
) async {
  if (config.trimProbeAppendCount <= 0) {
    return const <String, int>{
      'post_trim_message_count': 0,
      'trim_probe_elapsed_ms': 0,
    };
  }

  final imService = Get.find<ImService>();
  final currentWindow = List<MessageModel>.from(imService.currentMessages);
  if (currentWindow.isEmpty) {
    return const <String, int>{
      'post_trim_message_count': 0,
      'trim_probe_elapsed_ms': 0,
    };
  }

  var loadedStartIndex = allMessages.length - currentWindow.length;
  final stopwatch = Stopwatch()..start();
  final mutableWindow = List<MessageModel>.from(currentWindow);
  for (var i = 0; i < config.trimProbeAppendCount; i++) {
    final olderChunkEnd = loadedStartIndex;
    if (olderChunkEnd <= 0) {
      break;
    }
    final olderChunkStart = math.max(
      0,
      olderChunkEnd - _simulatedMessagePageSize,
    );
    final olderChunk = allMessages.sublist(olderChunkStart, olderChunkEnd);
    loadedStartIndex = olderChunkStart;
    mutableWindow.insertAll(0, olderChunk);
    final overflow = mutableWindow.length - _simulatedResidentMessageCap;
    if (overflow > 0) {
      mutableWindow.removeRange(
        mutableWindow.length - overflow,
        mutableWindow.length,
      );
    }
  }
  stopwatch.stop();
  imService.currentMessages.assignAll(mutableWindow);

  return <String, int>{
    'post_trim_message_count': mutableWindow.length,
    'trim_probe_elapsed_ms': stopwatch.elapsedMilliseconds,
  };
}

int _percentileInt(List<int> values, double percentile) {
  if (values.isEmpty) {
    return 0;
  }
  final sorted = List<int>.from(values)..sort();
  final p = percentile.clamp(0.0, 100.0);
  final rank = ((p / 100.0) * (sorted.length - 1)).round();
  return sorted[rank];
}

double _meanInt(List<int> values) {
  if (values.isEmpty) {
    return 0;
  }
  final total = values.fold<int>(0, (sum, item) => sum + item);
  return total / values.length;
}

void _openChatRoute(BuildContext context) {
  Navigator.of(context).push<void>(
    PageRouteBuilder<void>(
      transitionDuration: const Duration(milliseconds: 250),
      reverseTransitionDuration: const Duration(milliseconds: 250),
      pageBuilder: (_, __, ___) => ChatView(),
      transitionsBuilder: (_, animation, __, child) {
        final slide = Tween<Offset>(begin: const Offset(1, 0), end: Offset.zero)
            .animate(
              CurvedAnimation(parent: animation, curve: Curves.easeOutCubic),
            );
        return SlideTransition(position: slide, child: child);
      },
    ),
  );
}

Future<_ChatUiSample> _runUiIteration(
  WidgetTester tester,
  _BenchConfig config,
  int iteration,
) async {
  final sessionId = 'bench_session_$iteration';
  final messages = _buildMessages(
    count: config.messageCount,
    length: config.messageLength,
    markdownRatio: config.markdownRatio,
    sessionId: sessionId,
    senderPoolSize: config.senderPoolSize,
  );

  await _prepareMessagesForIteration(config, messages);

  if (Get.isRegistered<ChatController>()) {
    Get.delete<ChatController>(force: true);
  }
  final controller = Get.put(ChatController());
  controller.sessionId = sessionId;
  controller.chatTitle = sessionId;
  controller.chatType = config.chatType;
  if (config.chatType == 'group' && config.mentionItems > 0) {
    final items = List<Map<String, dynamic>>.generate(config.mentionItems, (
      index,
    ) {
      final memberId = '${3000 + index}';
      return <String, dynamic>{
        'member_id': memberId,
        'member_type': 1,
        'nickname': 'mention_$memberId',
      };
    });
    controller.filteredMentionList.assignAll(items);
    controller.showMentionList.value = true;
  }

  await tester.pumpWidget(
    GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('en', 'US'),
      fallbackLocale: const Locale('en', 'US'),
      home: Builder(
        builder: (context) {
          return Scaffold(
            body: Center(
              child: ElevatedButton(
                key: const ValueKey('bench_open_chat_button'),
                onPressed: () => _openChatRoute(context),
                child: const Text('Open Chat'),
              ),
            ),
          );
        },
      ),
    ),
  );
  await tester.pump();

  final openStopwatch = Stopwatch()..start();
  await tester.tap(find.byKey(const ValueKey('bench_open_chat_button')));
  await tester.pump();
  final openFirstFrameMs = openStopwatch.elapsedMilliseconds;
  await tester.pumpAndSettle();
  final openSettleMs = openStopwatch.elapsedMilliseconds;
  openStopwatch.stop();

  final visibleBubbleCount = find.byType(MessageBubble).evaluate().length;
  final visibleAstCount = find.byType(ChatMarkdownAstView).evaluate().length;
  final visibleSelectableTextCount = find
      .byType(SelectableText)
      .evaluate()
      .length;
  final visibleMsgIndexes = find
      .byType(MessageBubble)
      .evaluate()
      .map((element) => element.widget)
      .whereType<MessageBubble>()
      .map((bubble) => _extractBenchMessageIndex(bubble.msgId))
      .where((index) => index >= 0)
      .toList(growable: false);
  final visibleMsgIndexMin = visibleMsgIndexes.isEmpty
      ? -1
      : visibleMsgIndexes.reduce(math.min);
  final visibleMsgIndexMax = visibleMsgIndexes.isEmpty
      ? -1
      : visibleMsgIndexes.reduce(math.max);

  final imService = Get.find<ImService>();
  final loadedMessageCount = imService.currentMessages.length;
  final hasOlderMessages = config.messageCount > loadedMessageCount;

  final scrollStopwatch = Stopwatch()..start();
  if (controller.scrollController.hasClients) {
    controller.scrollController.jumpTo(0);
    await tester.pump();
    final max = controller.scrollController.position.maxScrollExtent;
    controller.scrollController.jumpTo(max);
  }
  await tester.pumpAndSettle();
  scrollStopwatch.stop();
  final keyboardActivateSettleMs = await _measureKeyboardActivateSettleMs(
    tester,
    controller,
  );

  final trimProbeResult = await _runTrimProbe(config, messages);
  if (config.trimProbeAppendCount > 0) {
    await tester.pump(const Duration(milliseconds: 120));
  }

  await tester.pumpWidget(const SizedBox.shrink());
  await tester.pump();
  Get.delete<ChatController>(force: true);

  return _ChatUiSample(
    iteration: iteration,
    openFirstFrameMs: openFirstFrameMs,
    openSettleMs: openSettleMs,
    scrollSettleMs: scrollStopwatch.elapsedMilliseconds,
    keyboardActivateSettleMs: keyboardActivateSettleMs,
    visibleBubbleCount: visibleBubbleCount,
    visibleAstCount: visibleAstCount,
    visibleSelectableTextCount: visibleSelectableTextCount,
    loadedMessageCount: loadedMessageCount,
    hasOlderMessages: hasOlderMessages,
    visibleMsgIndexMin: visibleMsgIndexMin,
    visibleMsgIndexMax: visibleMsgIndexMax,
    postTrimMessageCount: trimProbeResult['post_trim_message_count'] ?? 0,
    trimProbeElapsedMs: trimProbeResult['trim_probe_elapsed_ms'] ?? 0,
  );
}

Map<String, dynamic> _summarizeUiSamples(
  _BenchConfig config,
  List<_ChatUiSample> samples,
) {
  final openFirstFrameMsList = samples
      .map((sample) => sample.openFirstFrameMs)
      .toList(growable: false);
  final openSettleMsList = samples
      .map((sample) => sample.openSettleMs)
      .toList(growable: false);
  final scrollSettleMsList = samples
      .map((sample) => sample.scrollSettleMs)
      .toList(growable: false);
  final keyboardActivateSettleMsList = samples
      .map((sample) => sample.keyboardActivateSettleMs)
      .toList(growable: false);
  final visibleBubbleList = samples
      .map((sample) => sample.visibleBubbleCount)
      .toList(growable: false);
  final visibleAstList = samples
      .map((sample) => sample.visibleAstCount)
      .toList(growable: false);
  final selectableList = samples
      .map((sample) => sample.visibleSelectableTextCount)
      .toList(growable: false);
  final loadedMessageCountList = samples
      .map((sample) => sample.loadedMessageCount)
      .toList(growable: false);
  final visibleMsgIndexMinList = samples
      .map((sample) => sample.visibleMsgIndexMin)
      .where((value) => value >= 0)
      .toList(growable: false);
  final visibleMsgIndexMaxList = samples
      .map((sample) => sample.visibleMsgIndexMax)
      .where((value) => value >= 0)
      .toList(growable: false);
  final hasOlderMessagesCount = samples
      .where((sample) => sample.hasOlderMessages)
      .length;
  final postTrimMessageCountList = samples
      .map((sample) => sample.postTrimMessageCount)
      .where((value) => value > 0)
      .toList(growable: false);
  final trimProbeElapsedList = samples
      .map((sample) => sample.trimProbeElapsedMs)
      .where((value) => value > 0)
      .toList(growable: false);

  return {
    'scenario': 'chat_page_open_and_parse',
    'load_mode': config.loadMode.wireValue,
    'message_count': config.messageCount,
    'message_length': config.messageLength,
    'markdown_ratio': config.markdownRatio,
    'chat_type': config.chatType,
    'sender_pool_size': config.senderPoolSize,
    'mention_items': config.mentionItems,
    'iterations': samples.length,
    'open_first_frame_ms_p50': _percentileInt(openFirstFrameMsList, 50),
    'open_first_frame_ms_p95': _percentileInt(openFirstFrameMsList, 95),
    'open_settle_ms_p50': _percentileInt(openSettleMsList, 50),
    'open_settle_ms_p95': _percentileInt(openSettleMsList, 95),
    'scroll_settle_ms_p50': _percentileInt(scrollSettleMsList, 50),
    'scroll_settle_ms_p95': _percentileInt(scrollSettleMsList, 95),
    'keyboard_activate_settle_ms_p50': _percentileInt(
      keyboardActivateSettleMsList,
      50,
    ),
    'keyboard_activate_settle_ms_p95': _percentileInt(
      keyboardActivateSettleMsList,
      95,
    ),
    'visible_bubble_count_p50': _percentileInt(visibleBubbleList, 50),
    'visible_ast_count_p50': _percentileInt(visibleAstList, 50),
    'visible_selectable_text_count_p50': _percentileInt(selectableList, 50),
    'loaded_message_count_p50': _percentileInt(loadedMessageCountList, 50),
    'has_older_messages_ratio': (hasOlderMessagesCount / samples.length)
        .toStringAsFixed(2),
    'visible_msg_index_min_p50': visibleMsgIndexMinList.isEmpty
        ? -1
        : _percentileInt(visibleMsgIndexMinList, 50),
    'visible_msg_index_max_p50': visibleMsgIndexMaxList.isEmpty
        ? -1
        : _percentileInt(visibleMsgIndexMaxList, 50),
    'post_trim_message_count_p50': postTrimMessageCountList.isEmpty
        ? 0
        : _percentileInt(postTrimMessageCountList, 50),
    'trim_probe_elapsed_ms_p50': trimProbeElapsedList.isEmpty
        ? 0
        : _percentileInt(trimProbeElapsedList, 50),
    'open_settle_ms_mean': _meanInt(openSettleMsList).toStringAsFixed(2),
    'samples': samples.map((sample) => sample.toJson()).toList(growable: false),
  };
}

Map<String, dynamic> _runParseBenchmark(_BenchConfig config) {
  final parseCount = math.min(config.parseSampleCount, config.messageCount);
  final messages = _buildMessages(
    count: parseCount,
    length: config.messageLength,
    markdownRatio: config.markdownRatio,
    sessionId: 'bench_parse',
    senderPoolSize: config.senderPoolSize,
  );
  final pipeline = ChatMarkdownPipeline(
    normalizer: const ChatMarkdownNormalizer(),
    parser: ChatMarkdownDialect.buildParserAdapter(),
  );

  var totalChars = 0;
  var richRenderCount = 0;

  final previewStopwatch = Stopwatch()..start();
  for (final message in messages) {
    final preview = pipeline.preparePreview(message.content);
    totalChars += preview.normalizedText.length;
  }
  previewStopwatch.stop();

  final finalStopwatch = Stopwatch()..start();
  for (final message in messages) {
    final renderState = pipeline.prepareFinalRender(message.content);
    if (renderState.shouldUseMarkdown) {
      richRenderCount += 1;
    }
  }
  finalStopwatch.stop();

  final finalElapsedMs = finalStopwatch.elapsedMilliseconds;
  final throughputPerSecond = finalElapsedMs <= 0
      ? 0.0
      : parseCount * 1000.0 / finalElapsedMs;

  return {
    'scenario': 'markdown_pipeline_parse',
    'load_mode': config.loadMode.wireValue,
    'parse_sample_count': parseCount,
    'message_length': config.messageLength,
    'markdown_ratio': config.markdownRatio,
    'preview_total_ms': previewStopwatch.elapsedMilliseconds,
    'final_total_ms': finalElapsedMs,
    'total_chars': totalChars,
    'rich_render_count': richRenderCount,
    'final_throughput_msg_per_s': throughputPerSecond.toStringAsFixed(2),
  };
}

void _installBenchDependencies(_BenchConfig config) {
  Get.testMode = true;
  Get.reset();
  Get.put<ImService>(_BenchPrefilledImService());
  Get.put<AuthService>(_BenchAuthService());
  Get.put<AgentService>(_BenchAgentService());
  Get.put<SessionService>(_BenchSessionService(config));
  Get.put<OssService>(_BenchOssService());
}

void main() {
  final config = _BenchConfig.fromEnvironment();

  setUp(() {
    _installBenchDependencies(config);
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('benchmark: chat page open/render on large messages', (
    tester,
  ) async {
    final samples = <_ChatUiSample>[];
    for (var i = 0; i < config.iterations; i++) {
      final sample = await _runUiIteration(tester, config, i + 1);
      samples.add(sample);
      // ignore: avoid_print
      print('BENCH_CHAT_SAMPLE ${jsonEncode(sample.toJson())}');
    }

    final summary = _summarizeUiSamples(config, samples);
    // ignore: avoid_print
    print('BENCH_CHAT_SUMMARY ${jsonEncode(summary)}');

    expect(samples, isNotEmpty);
    expect(samples.first.visibleBubbleCount, greaterThan(0));
  }, skip: !config.enabled);

  test('benchmark: markdown parse throughput', () {
    final summary = _runParseBenchmark(config);
    // ignore: avoid_print
    print('BENCH_PARSE_SUMMARY ${jsonEncode(summary)}');

    expect(summary['parse_sample_count'] as int, greaterThan(0));
  }, skip: !config.enabled);
}
