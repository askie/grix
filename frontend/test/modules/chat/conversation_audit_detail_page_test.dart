import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/agent_service.dart';
import 'package:grix/data/providers/im_service.dart';
import 'package:grix/modules/chat/widgets/conversation_audit_detail_page.dart';

class _AuditContentRequest {
  const _AuditContentRequest({
    required this.sessionId,
    required this.msgId,
    required this.contentId,
    required this.agentId,
    required this.revision,
    required this.cursor,
    required this.maxBytes,
  });

  final String sessionId;
  final String msgId;
  final String contentId;
  final String? agentId;
  final int? revision;
  final String? cursor;
  final int? maxBytes;
}

class _FakeAuditImService extends ImService {
  List<Map<String, dynamic>> spans = [
    {
      'kind': 'llm_request',
      'name': '审计节点',
      'status': 'failed',
      'duration_ms': 1532,
      'usage': {
        'input': {'total': 201818},
        'output': {'total': 1010},
      },
    },
  ];
  List<Map<String, dynamic>> manifestContentRefs = <Map<String, dynamic>>[];
  Object? manifestProvider;
  Object? manifestModelId;
  Map<String, Object?> manifestExtras = <String, Object?>{};
  List<Map<String, dynamic>> auditTargets = <Map<String, dynamic>>[];
  final Map<String, List<Object>> contentResponses = <String, List<Object>>{};
  final List<_AuditContentRequest> contentRequests = <_AuditContentRequest>[];
  Completer<Map<String, dynamic>>? nextManifestResponse;

  @override
  Future<Map<String, dynamic>> requestConversationAuditManifest({
    required String sessionId,
    required String msgId,
    String? agentId,
  }) async {
    final pending = nextManifestResponse;
    if (pending != null) {
      nextManifestResponse = null;
      return pending.future;
    }
    if (agentId == null && auditTargets.isNotEmpty) {
      return {'state': 'selection_required', 'targets': auditTargets};
    }
    return buildManifestResponse();
  }

  Map<String, dynamic> buildManifestResponse({int revision = 2}) {
    return {
      'state': 'ready',
      'revision': revision,
      'result': {
        'audit_id': 'audit-1',
        'status': 'failed',
        'revision': revision,
        'has_spans': true,
        if (manifestProvider != null) 'provider': manifestProvider,
        if (manifestModelId != null) 'model_id': manifestModelId,
        ...manifestExtras,
        'statistics': {
          'total_usage': {
            'input': {'total': 201818, 'cacheRead': 162304},
            'output': {'total': 1010},
            'total_processed': 202828,
          },
        },
        'content_refs': manifestContentRefs,
      },
    };
  }

  @override
  Future<Map<String, dynamic>> requestConversationAuditSpans({
    required String sessionId,
    required String msgId,
    String? agentId,
    int? revision,
    String? cursor,
    int? limit,
  }) async {
    return {
      'state': 'ready',
      'revision': 2,
      'result': {'items': spans, 'next_cursor': null, 'has_more': false},
    };
  }

  @override
  Future<Map<String, dynamic>> requestConversationAuditContentChunk({
    required String sessionId,
    required String msgId,
    required String contentId,
    String? agentId,
    int? revision,
    String? cursor,
    int? maxBytes,
  }) async {
    contentRequests.add(
      _AuditContentRequest(
        sessionId: sessionId,
        msgId: msgId,
        contentId: contentId,
        agentId: agentId,
        revision: revision,
        cursor: cursor,
        maxBytes: maxBytes,
      ),
    );
    final responses = contentResponses[contentId];
    if (responses == null || responses.isEmpty) {
      throw StateError('missing fake response for $contentId');
    }
    final response = responses.removeAt(0);
    if (response is Future<Map<String, dynamic>>) return response;
    if (response is Map<String, dynamic>) return response;
    throw response;
  }
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late _FakeAuditImService imService;

  setUp(() {
    Get.testMode = true;
    Get.reset();
    imService = _FakeAuditImService();
    Get.put<ImService>(imService);
  });

  tearDown(() {
    Get.reset();
  });

  testWidgets('renders a failed audit span on a narrow mobile viewport', (
    WidgetTester tester,
  ) async {
    tester.view.physicalSize = const Size(390, 844);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: const ConversationAuditDetailPage(
          sessionId: 'session-1',
          msgId: '1001',
        ),
      ),
    );
    await tester.pumpAndSettle();
    await tester.drag(find.byType(ListView), const Offset(0, -700));
    await tester.pumpAndSettle();

    expect(tester.takeException(), isNull);
    expect(find.text('模型调用'), findsWidgets);
    expect(find.text('异常'), findsWidgets);
    expect(find.text('1.53s'), findsOneWidget);
    expect(find.text('仅展示已采集的真实调用；缺失耗时不会估算。'), findsOneWidget);
  });

  testWidgets('localizes the audit send action in English', (
    WidgetTester tester,
  ) async {
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('en', 'US'),
        home: const ConversationAuditDetailPage(
          sessionId: 'session-1',
          msgId: '1001',
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Conversation audit'), findsOneWidget);
    expect(find.text('Total duration'), findsOneWidget);
    expect(find.byTooltip('Send audit content to AI'), findsOneWidget);
    expect(find.byTooltip('发送审计内容给 AI'), findsNothing);
  });

  testWidgets('resolves audit target names from accessible agents', (
    WidgetTester tester,
  ) async {
    imService.auditTargets = [
      {'agent_id': 'agent-owned', 'state': 'ready', 'revision': 1},
      {'agent_id': 'agent-shared', 'state': 'partial', 'revision': 1},
      {'agent_id': 'agent-unknown', 'state': 'partial', 'revision': 1},
    ];
    final agentService = AgentService();
    agentService.agents.assignAll([
      AgentModel(id: 'agent-owned', agentName: '自有助手'),
    ]);
    agentService.sharedAgents.assignAll([
      AgentModel(id: 'agent-shared', agentName: '共享助手'),
    ]);
    agentService.hasLoaded.value = true;
    Get.put<AgentService>(agentService);

    await _pumpAuditPage(tester);

    expect(find.text('自有助手'), findsOneWidget);
    expect(find.text('共享助手'), findsOneWidget);
    expect(find.text('Agent agent-unknown'), findsOneWidget);
    expect(find.text('Agent agent-owned'), findsNothing);
    expect(find.text('Agent agent-shared'), findsNothing);
  });

  testWidgets('labels unavailable and imprecise audit durations distinctly', (
    WidgetTester tester,
  ) async {
    imService.spans = [
      {'kind': 'llm_request', 'name': '模型快照', 'status': 'completed'},
      {
        'kind': 'tool_call',
        'name': '同毫秒工具',
        'status': 'completed',
        'duration_ms': 0,
        'started_at': '2026-07-28T09:49:03.461Z',
        'ended_at': '2026-07-28T09:49:03.461Z',
      },
      {
        'kind': 'tool_call',
        'name': '真实零耗时工具',
        'status': 'completed',
        'duration_ms': 0,
        'started_at': '2026-07-28T09:49:03.461Z',
        'ended_at': '2026-07-28T09:49:03.462Z',
      },
    ];

    await _pumpAuditPage(tester);
    await tester.drag(find.byType(ListView), const Offset(0, -700));
    await tester.pumpAndSettle();

    expect(find.text('Codex 未提供请求耗时'), findsOneWidget);
    expect(find.text('耗时不精确'), findsOneWidget);
    expect(find.text('0ms'), findsOneWidget);
  });

  testWidgets('shows real client labels for ACP audit manifests', (
    WidgetTester tester,
  ) async {
    Future<void> verify({
      required Object? provider,
      required Object? modelId,
      required Map<String, Object?> extras,
      required String expectedLabel,
    }) async {
      imService.manifestProvider = provider;
      imService.manifestModelId = modelId;
      imService.manifestExtras = extras;

      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
      await _pumpAuditPage(tester);

      expect(find.text(expectedLabel), findsOneWidget);
      if (modelId != null) {
        expect(find.text(modelId.toString()), findsOneWidget);
      }
      expect(find.text('acp'), findsNothing);
    }

    await verify(
      provider: 'acp',
      modelId: 'kimi-k2',
      extras: const <String, Object?>{},
      expectedLabel: 'Kimi',
    );
    await verify(
      provider: 'acp',
      modelId: null,
      extras: const <String, Object?>{'provider_key': 'claude/base'},
      expectedLabel: 'Claude',
    );
    await verify(
      provider: 'acp',
      modelId: 'gemini-2.5-pro',
      extras: const <String, Object?>{},
      expectedLabel: 'Gemini',
    );
  });

  testWidgets(
    'renders token chart controls and opens span JSON from a bar tap',
    (WidgetTester tester) async {
      imService.spans = [
        {
          'kind': 'llm_request',
          'name': '模型节点',
          'status': 'completed',
          'duration_ms': 20,
          'usage': {
            'input': {'total': 1200},
            'output': {'total': 320},
          },
        },
        {
          'kind': 'tool_call',
          'name': '工具节点',
          'status': 'completed',
          'duration_ms': 8,
          'usage': {
            'input': {'total': '80'},
            'output': {'total': 40},
          },
        },
      ];

      await _pumpAuditPage(tester);
      final chartFinder = find.byKey(
        const ValueKey('audit-token-timeline-chart'),
      );
      await tester.scrollUntilVisible(
        chartFinder,
        300,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();

      final outputToggleFinder = find.byKey(
        const ValueKey('audit-token-chart-output-toggle'),
      );
      await Scrollable.ensureVisible(
        tester.element(outputToggleFinder),
        alignment: 0.3,
      );
      await tester.pumpAndSettle();

      expect(
        tester
            .widget<FilterChip>(
              find.byKey(const ValueKey('audit-token-chart-input-toggle')),
            )
            .selected,
        isTrue,
      );
      expect(tester.widget<FilterChip>(outputToggleFinder).selected, isTrue);

      await tester.tap(outputToggleFinder);
      await tester.pumpAndSettle();
      expect(tester.widget<FilterChip>(outputToggleFinder).selected, isFalse);

      final inputToggleFinder = find.byKey(
        const ValueKey('audit-token-chart-input-toggle'),
      );
      await tester.tap(inputToggleFinder);
      await tester.pumpAndSettle();
      expect(tester.widget<FilterChip>(inputToggleFinder).selected, isTrue);
      expect(tester.widget<FilterChip>(outputToggleFinder).selected, isFalse);
      expect(find.text('这些调用节点没有采集到 token 用量。'), findsNothing);

      await Scrollable.ensureVisible(
        tester.element(chartFinder),
        alignment: 0.4,
      );
      await tester.pumpAndSettle();
      await tester.tapAt(tester.getCenter(chartFinder));
      await tester.pumpAndSettle();

      expect(find.text('节点完整数据'), findsOneWidget);
      expect(find.textContaining('"name": "工具节点"'), findsOneWidget);
    },
  );

  testWidgets(
    'loads every content chunk when the span sheet opens and copies enriched JSON',
    (WidgetTester tester) async {
      imService.spans = [
        {
          'kind': 'turn',
          'name': '带正文节点',
          'status': 'completed',
          'output_refs': [
            {'bytes': 12, 'content_id': 'output-1', 'kind': 'final_response'},
          ],
        },
      ];
      imService.contentResponses['output-1'] = [
        {
          'result': {'value': '关键', 'next_cursor': 'cursor-2', 'eof': false},
        },
        {
          'result': {'value': '正文', 'next_cursor': null, 'eof': true},
        },
      ];

      await _pumpAuditPage(tester);
      await _openSpanSheet(tester, '带正文节点');

      expect(imService.contentRequests, hasLength(2));
      expect(imService.contentRequests[0].sessionId, 'session-1');
      expect(imService.contentRequests[0].msgId, '1001');
      expect(imService.contentRequests[0].contentId, 'output-1');
      expect(imService.contentRequests[0].revision, 2);
      expect(imService.contentRequests[0].cursor, isNull);
      expect(imService.contentRequests[0].maxBytes, 131072);
      expect(imService.contentRequests[1].cursor, 'cursor-2');

      final contentIdFinder = find.textContaining('"content_id": "output-1"');
      final contentFinder = find.textContaining('"content": "关键正文"');
      expect(contentIdFinder, findsOneWidget);
      expect(contentFinder, findsOneWidget);
      expect(
        tester.getTopLeft(contentFinder).dy,
        greaterThan(tester.getTopLeft(contentIdFinder).dy),
      );

      String? copiedText;
      tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
        SystemChannels.platform,
        (MethodCall call) async {
          if (call.method == 'Clipboard.setData') {
            copiedText =
                (call.arguments as Map<Object?, Object?>)['text'] as String?;
          }
          return null;
        },
      );
      addTearDown(
        () => tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(
          SystemChannels.platform,
          null,
        ),
      );

      await tester.tap(find.text('复制完整 JSON'));
      await tester.pump();

      expect(
        copiedText,
        matches(RegExp(r'"content_id": "output-1",\s+"content": "关键正文"')),
      );
      await tester.pump(const Duration(seconds: 3));
      await tester.pumpAndSettle();
    },
  );

  testWidgets('collapses long loaded content until the user expands it', (
    WidgetTester tester,
  ) async {
    final longContent = List.filled(90, '长正文片段').join();
    imService.spans = [
      {
        'kind': 'turn',
        'name': '长正文节点',
        'status': 'completed',
        'output_refs': [
          {'content_id': 'long-output', 'kind': 'final_response'},
        ],
      },
    ];
    imService.contentResponses['long-output'] = [
      {
        'result': {'value': longContent, 'next_cursor': null, 'eof': true},
      },
    ];

    await _pumpAuditPage(tester);
    await _openSpanSheet(tester, '长正文节点');

    const contentKey = ValueKey<String>(
      r'audit-span-content-value:$.output_refs[0].content',
    );
    const toggleKey = ValueKey<String>(
      r'audit-span-content-toggle:$.output_refs[0].content',
    );
    expect(tester.widget<Text>(find.byKey(contentKey)).maxLines, 8);
    expect(find.text('展开'), findsOneWidget);
    final collapsedHeight = tester.getSize(find.byKey(contentKey)).height;

    await tester.tap(find.byKey(toggleKey));
    await tester.pumpAndSettle();

    final expandedText = tester.widget<Text>(find.byKey(contentKey));
    expect(expandedText.maxLines, isNull);
    expect(expandedText.overflow, TextOverflow.clip);
    expect(
      tester.getSize(find.byKey(contentKey)).height,
      greaterThan(collapsedHeight),
    );
    expect(find.text('收起'), findsOneWidget);
  });

  testWidgets('keeps an unbreakable content token inside the sheet viewport', (
    WidgetTester tester,
  ) async {
    final longToken = List.filled(1200, 'A').join();
    imService.spans = [
      {
        'kind': 'turn',
        'name': '不可断行正文节点',
        'status': 'completed',
        'output_refs': [
          {'content_id': 'token-output', 'kind': 'final_response'},
        ],
      },
    ];
    imService.contentResponses['token-output'] = [
      {
        'result': {'value': longToken, 'next_cursor': null, 'eof': true},
      },
    ];

    await _pumpAuditPage(tester);
    await _openSpanSheet(tester, '不可断行正文节点');

    const contentKey = ValueKey<String>(
      r'audit-span-content-value:$.output_refs[0].content',
    );
    const toggleKey = ValueKey<String>(
      r'audit-span-content-toggle:$.output_refs[0].content',
    );
    expect(find.byKey(toggleKey), findsOneWidget);
    expect(
      tester.widget<Text>(find.byKey(contentKey)).overflow,
      TextOverflow.ellipsis,
    );

    await tester.tap(find.byKey(toggleKey));
    await tester.pumpAndSettle();

    expect(
      tester.widget<Text>(find.byKey(contentKey)).overflow,
      TextOverflow.clip,
    );
    expect(tester.takeException(), isNull);
  });

  testWidgets('keeps the span sheet usable on a short viewport', (
    WidgetTester tester,
  ) async {
    imService.spans = [
      {
        'kind': 'turn',
        'name': '短屏节点',
        'status': 'completed',
        'output_refs': [
          {'content_id': 'short-screen-output', 'kind': 'final_response'},
        ],
      },
    ];
    imService.contentResponses['short-screen-output'] = [
      {
        'result': {'value': '正文', 'next_cursor': null, 'eof': true},
      },
    ];

    await _pumpAuditPage(tester, size: const Size(390, 360));
    await _openSpanSheet(tester, '短屏节点');

    expect(tester.takeException(), isNull);
    expect(find.text('复制完整 JSON'), findsOneWidget);
    expect(find.textContaining('"content": "正文"'), findsOneWidget);
  });

  testWidgets(
    'disables full JSON copy until failed content is retried successfully',
    (WidgetTester tester) async {
      imService.spans = [
        {
          'kind': 'turn',
          'name': '部分失败节点',
          'status': 'completed',
          'output_refs': [
            {'content_id': 'good-output', 'kind': 'tool_result'},
            {'content_id': 'bad-output', 'kind': 'final_response'},
          ],
        },
      ];
      imService.contentResponses['good-output'] = [
        {
          'result': {'value': '工具结果', 'next_cursor': null, 'eof': true},
        },
      ];
      imService.contentResponses['bad-output'] = [
        StateError('connector offline'),
        {
          'result': {'value': '最终正文', 'next_cursor': null, 'eof': true},
        },
      ];

      await _pumpAuditPage(tester);
      await _openSpanSheet(tester, '部分失败节点');

      expect(find.textContaining('bad-output:'), findsOneWidget);
      expect(find.textContaining('"content": "工具结果"'), findsOneWidget);
      var copyButton = tester.widget<FilledButton>(
        find.ancestor(
          of: find.text('复制完整 JSON'),
          matching: find.byType(FilledButton),
        ),
      );
      expect(copyButton.onPressed, isNull);

      await tester.tap(find.text('重试'));
      await tester.pumpAndSettle();

      expect(find.textContaining('bad-output:'), findsNothing);
      expect(find.textContaining('"content": "最终正文"'), findsOneWidget);
      copyButton = tester.widget<FilledButton>(
        find.ancestor(
          of: find.text('复制完整 JSON'),
          matching: find.byType(FilledButton),
        ),
      );
      expect(copyButton.onPressed, isNotNull);
    },
  );

  testWidgets(
    'shares an in-flight content chunk between the page and span sheet',
    (WidgetTester tester) async {
      final firstChunk = Completer<Map<String, dynamic>>();
      imService.manifestContentRefs = [
        {'content_id': 'shared-output', 'kind': 'final_response', 'bytes': 8},
      ];
      imService.spans = [
        {
          'kind': 'turn',
          'name': '共享正文节点',
          'status': 'completed',
          'output_refs': [
            {'content_id': 'shared-output', 'kind': 'final_response'},
          ],
        },
      ];
      imService.contentResponses['shared-output'] = [
        firstChunk.future,
        {
          'result': {'value': '后半段', 'next_cursor': null, 'eof': true},
        },
      ];

      await _pumpAuditPage(tester);
      final contentSection = find.text('查看审计文本与原始事件');
      await tester.scrollUntilVisible(
        contentSection,
        300,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      await tester.tap(contentSection);
      await tester.pumpAndSettle();
      final loadText = find.text('加载文本');
      await tester.scrollUntilVisible(
        loadText,
        120,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pumpAndSettle();
      await tester.tap(loadText);
      await tester.pump();
      expect(imService.contentRequests, hasLength(1));

      final spanFinder = find.text('共享正文节点');
      await tester.scrollUntilVisible(
        spanFinder,
        -300,
        scrollable: find.byType(Scrollable).first,
      );
      await tester.pump();
      await tester.tap(spanFinder);
      await tester.pump();
      expect(imService.contentRequests, hasLength(1));

      firstChunk.complete({
        'result': {'value': '前半段', 'next_cursor': 'cursor-2', 'eof': false},
      });
      await tester.pump();
      await tester.pumpAndSettle();

      expect(imService.contentRequests, hasLength(2));
      expect(imService.contentRequests[0].cursor, isNull);
      expect(imService.contentRequests[1].cursor, 'cursor-2');
      expect(find.textContaining('"content": "前半段后半段"'), findsOneWidget);
    },
  );

  testWidgets('ignores an old content response after the page refreshes', (
    WidgetTester tester,
  ) async {
    final oldChunk = Completer<Map<String, dynamic>>();
    imService.spans = [
      {
        'kind': 'turn',
        'name': '刷新节点',
        'status': 'completed',
        'output_refs': [
          {'content_id': 'refresh-output', 'kind': 'final_response'},
        ],
      },
    ];
    imService.contentResponses['refresh-output'] = [
      oldChunk.future,
      {
        'result': {'value': '新版本正文', 'next_cursor': null, 'eof': true},
      },
    ];

    await _pumpAuditPage(tester);
    final spanFinder = find.text('刷新节点');
    await tester.scrollUntilVisible(
      spanFinder,
      300,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.tap(spanFinder);
    await tester.pump();
    await tester.pump(const Duration(milliseconds: 400));
    expect(imService.contentRequests, hasLength(1));

    await tester.tap(find.byIcon(Icons.close));
    await tester.pumpAndSettle();
    final scrollable = tester.state<ScrollableState>(
      find.byType(Scrollable).first,
    );
    scrollable.position.jumpTo(0);
    await tester.pump();
    await tester.drag(find.byType(ListView), const Offset(0, 320));
    await tester.pump();
    await tester.pumpAndSettle();

    oldChunk.complete({
      'result': {'value': '旧版本正文', 'next_cursor': null, 'eof': true},
    });
    await tester.pump();
    await _openSpanSheet(tester, '刷新节点');

    expect(imService.contentRequests, hasLength(2));
    expect(find.textContaining('"content": "新版本正文"'), findsOneWidget);
    expect(find.textContaining('旧版本正文'), findsNothing);
  });

  testWidgets('does not start old-revision content loads during refresh', (
    WidgetTester tester,
  ) async {
    final refreshedManifest = Completer<Map<String, dynamic>>();
    imService.manifestContentRefs = [
      {'content_id': 'refresh-window', 'kind': 'final_response', 'bytes': 8},
    ];
    imService.contentResponses['refresh-window'] = [
      {
        'result': {'value': '不应请求', 'next_cursor': null, 'eof': true},
      },
    ];

    await _pumpAuditPage(tester);
    final contentSection = find.text('查看审计文本与原始事件');
    await tester.scrollUntilVisible(
      contentSection,
      300,
      scrollable: find.byType(Scrollable).first,
    );
    await tester.pumpAndSettle();
    await tester.tap(contentSection);
    await tester.pumpAndSettle();

    final loadText = find.text('加载文本');
    final loadButton = tester.widget<TextButton>(
      find.ancestor(of: loadText, matching: find.byType(TextButton)),
    );
    imService.nextManifestResponse = refreshedManifest;
    final refreshFuture = tester
        .widget<RefreshIndicator>(find.byType(RefreshIndicator))
        .onRefresh();
    await tester.pump();
    expect(imService.nextManifestResponse, isNull);

    loadButton.onPressed!();
    await tester.pump();
    expect(imService.contentRequests, isEmpty);

    refreshedManifest.complete(imService.buildManifestResponse(revision: 3));
    await refreshFuture;
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
  });

  testWidgets('stops automatic loading when a chunk makes no byte progress', (
    WidgetTester tester,
  ) async {
    imService.spans = [
      {
        'kind': 'turn',
        'name': '异常游标节点',
        'status': 'completed',
        'output_refs': [
          {'content_id': 'stalled-output', 'kind': 'final_response'},
        ],
      },
    ];
    imService.contentResponses['stalled-output'] = [
      {
        'result': {'value': '', 'next_cursor': 'cursor-2', 'eof': false},
      },
    ];

    await _pumpAuditPage(tester);
    await _openSpanSheet(tester, '异常游标节点');

    expect(imService.contentRequests, hasLength(1));
    expect(find.textContaining('stalled-output:'), findsOneWidget);
    final copyButton = tester.widget<FilledButton>(
      find.ancestor(
        of: find.text('复制完整 JSON'),
        matching: find.byType(FilledButton),
      ),
    );
    expect(copyButton.onPressed, isNull);
  });

  testWidgets('keeps the original JSON when a span has no content id', (
    WidgetTester tester,
  ) async {
    await _pumpAuditPage(tester);
    await _openSpanSheet(tester, '审计节点');

    expect(imService.contentRequests, isEmpty);
    expect(
      find.byKey(const ValueKey('audit-span-content-loading')),
      findsNothing,
    );
    expect(find.textContaining('"name": "审计节点"'), findsOneWidget);
  });
}

Future<void> _pumpAuditPage(
  WidgetTester tester, {
  Size size = const Size(390, 844),
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.resetPhysicalSize);
  addTearDown(tester.view.resetDevicePixelRatio);

  await tester.pumpWidget(
    GetMaterialApp(
      translations: AppTranslations(),
      locale: const Locale('zh', 'CN'),
      home: const ConversationAuditDetailPage(
        sessionId: 'session-1',
        msgId: '1001',
      ),
    ),
  );
  await tester.pumpAndSettle();
}

Future<void> _openSpanSheet(WidgetTester tester, String spanName) async {
  final spanFinder = find.text(spanName);
  await tester.scrollUntilVisible(
    spanFinder,
    300,
    scrollable: find.byType(Scrollable).first,
  );
  await tester.pumpAndSettle();
  await tester.tap(spanFinder);
  await tester.pumpAndSettle();
}
