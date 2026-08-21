import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'dart:async';
import 'dart:collection';
import 'dart:convert';
import 'package:rxdart/rxdart.dart';
import 'package:get/get.dart';
import 'package:crypto/crypto.dart';
import '../../data/models/message_model.dart';
import '../../data/providers/local_db.dart';
import '../markdown/chat_markdown_engine.dart';
import '../markdown/chat_markdown_pipeline.dart';
import '../markdown/chat_markdown_render_cache_codec.dart';
import '../../app/themes/app_theme.dart';
import '../../app/settings/chat_font_size_service.dart';
import '../../modules/chat/message_cards/models/chat_message_card_data.dart';
import '../../modules/chat/message_cards/models/chat_message_card_type.dart';
import '../../modules/chat/message_cards/models/chat_message_card_action.dart';
import '../../modules/chat/message_cards/services/chat_message_card_codec.dart';
import '../../modules/chat/message_cards/widgets/chat_message_card_view.dart';
import '../../modules/chat/message_cards/models/chat_thinking_card_data.dart';
import '../../modules/chat/message_cards/widgets/chat_thinking_card_view.dart';
import '../../modules/chat/services/chat_managed_input.dart';
import '../utils/chat_message_attachment_codec.dart';
import '../utils/chat_message_content.dart';
import '../utils/chat_message_preview.dart';
import 'chat_selection_area.dart';
import 'chat_message_attachment_grid.dart';
import 'chat_markdown_view.dart';
import 'chat_dispatch_result_card.dart';
import 'stream_pending_indicator.dart';
import 'app_dialog_style.dart';

// 单例中心分发器：后续在此处接收 IM_Service 下发的各类流式片段
class MessageStreamController {
  static final Map<String, BehaviorSubject<String>> _streams = {};
  static final Map<String, StringBuffer> _buffers = {};
  static final Map<String, Timer> _flushTimers = {};
  static final LinkedHashMap<String, String> _recoverableSnapshots =
      LinkedHashMap<String, String>();
  static final Map<String, SplayTreeMap<int, String>> _orderedPendingChunks =
      {};
  static final Map<String, int> _nextChunkSeqByMsg = {};
  // 80ms ≈ 12fps，打字机效果足够流畅，同时给 Flutter Web JS 引擎足够喘息空间
  static const Duration _flushInterval = Duration(milliseconds: 80);
  static const int _maxRecoverableSnapshots = 256;

  static BehaviorSubject<String> getStream(String msgId) {
    if (!_streams.containsKey(msgId)) {
      _streams[msgId] = BehaviorSubject<String>.seeded('');
    }
    return _streams[msgId]!;
  }

  static void addChunk(String msgId, String chunk, {int? chunkSeq}) {
    if (chunk.isEmpty) return;
    if (chunkSeq != null && chunkSeq > 0) {
      _addOrderedChunk(msgId, chunkSeq, chunk);
      return;
    }
    _appendChunk(msgId, chunk);
  }

  static void _appendChunk(String msgId, String chunk) {
    final stream = getStream(msgId); // 自动创建，不丢弃
    final buffer = _buffers.putIfAbsent(
      msgId,
      () => StringBuffer(stream.value),
    );
    buffer.write(chunk);
    _cacheRecoverableSnapshot(msgId, buffer.toString());
    _flushTimers[msgId] ??= Timer(_flushInterval, () {
      _flushTimers.remove(msgId);
      final activeStream = _streams[msgId];
      final activeBuffer = _buffers[msgId];
      if (activeStream == null || activeBuffer == null) {
        return;
      }
      activeStream.add(activeBuffer.toString());
    });
  }

  static void _addOrderedChunk(String msgId, int chunkSeq, String chunk) {
    final expected = _nextChunkSeqByMsg.putIfAbsent(msgId, () => 1);
    if (chunkSeq < expected) {
      return;
    }

    final pending = _orderedPendingChunks.putIfAbsent(
      msgId,
      () => SplayTreeMap<int, String>(),
    );

    // 重复 chunk 直接丢弃，避免网络重传导致重复字串。
    pending.putIfAbsent(chunkSeq, () => chunk);
    // 保持严格顺序：缺失前序分片时不跳过，避免气泡里出现错序文本。
    _drainOrderedChunks(msgId);
  }

  static void _drainOrderedChunks(String msgId) {
    final pending = _orderedPendingChunks[msgId];
    if (pending == null || pending.isEmpty) {
      return;
    }

    var expected = _nextChunkSeqByMsg[msgId] ?? pending.firstKey()!;

    while (true) {
      final nextChunk = pending.remove(expected);
      if (nextChunk == null) {
        break;
      }
      _appendChunk(msgId, nextChunk);
      expected = expected + 1;
    }

    _nextChunkSeqByMsg[msgId] = expected;
    if (pending.isEmpty) {
      _orderedPendingChunks.remove(msgId);
      return;
    }
  }

  static bool hasActiveProducer(String msgId) {
    return _buffers.containsKey(msgId) ||
        _flushTimers.containsKey(msgId) ||
        _orderedPendingChunks.containsKey(msgId);
  }

  static String peekContent(String msgId) {
    if (msgId.isEmpty) {
      return '';
    }
    final buffered = _buffers[msgId]?.toString() ?? '';
    if (buffered.isNotEmpty) {
      return buffered;
    }
    final stream = _streams[msgId];
    if (stream == null) {
      return '';
    }
    return stream.value;
  }

  static String peekRecoverableContent(String msgId) {
    if (msgId.isEmpty) {
      return '';
    }
    final liveContent = peekContent(msgId);
    if (liveContent.isNotEmpty) {
      return liveContent;
    }
    return _recoverableSnapshots[msgId] ?? '';
  }

  static void _cacheRecoverableSnapshot(String msgId, String content) {
    if (msgId.isEmpty) {
      return;
    }
    final normalized = ChatMessageContent.unwrapStructuredText(content);
    if (normalized.trim().isEmpty) {
      return;
    }
    _recoverableSnapshots.remove(msgId);
    _recoverableSnapshots[msgId] = content;
    while (_recoverableSnapshots.length > _maxRecoverableSnapshots) {
      _recoverableSnapshots.remove(_recoverableSnapshots.keys.first);
    }
  }

  /// 立即丢弃一个 stream，清理所有相关状态，不向订阅者 emit 任何内容。
  /// 用于停止场景：消息正在被移除，不需要触发 onDone 的最终渲染。
  static void discard(String msgId) {
    _orderedPendingChunks.remove(msgId);
    _nextChunkSeqByMsg.remove(msgId);
    _flushTimers.remove(msgId)?.cancel();
    _buffers.remove(msgId);
    _recoverableSnapshots.remove(msgId);
    _streams.remove(msgId)?.close();
  }

  static void finish(String msgId, String finalContent) {
    final recoverableSnapshot = finalContent.isNotEmpty
        ? finalContent
        : _buffers[msgId]?.toString() ??
              _streams[msgId]?.value ??
              _recoverableSnapshots[msgId] ??
              '';
    _orderedPendingChunks.remove(msgId);
    _nextChunkSeqByMsg.remove(msgId);
    _flushTimers.remove(msgId)?.cancel();
    _buffers.remove(msgId);
    final stream = _streams.remove(msgId);
    _cacheRecoverableSnapshot(msgId, recoverableSnapshot);
    if (stream == null) return;
    stream.add(finalContent);
    stream.close();
  }

  static void transfer(String fromMsgId, String toMsgId) {
    if (fromMsgId.isEmpty || toMsgId.isEmpty || fromMsgId == toMsgId) {
      return;
    }

    final sourceStream = _streams.remove(fromMsgId);
    final sourceBuffer = _buffers.remove(fromMsgId);
    _flushTimers.remove(fromMsgId)?.cancel();
    final sourcePending = _orderedPendingChunks.remove(fromMsgId);
    final sourceNextChunkSeq = _nextChunkSeqByMsg.remove(fromMsgId);
    final sourceRecoverableSnapshot =
        _recoverableSnapshots.remove(fromMsgId) ?? '';

    final mergedContent = sourceBuffer?.toString() ?? sourceStream?.value ?? '';
    final targetStream = getStream(toMsgId);
    if (mergedContent.isNotEmpty && targetStream.value != mergedContent) {
      targetStream.add(mergedContent);
    }
    if (mergedContent.isNotEmpty) {
      _cacheRecoverableSnapshot(toMsgId, mergedContent);
    } else if (sourceRecoverableSnapshot.isNotEmpty) {
      _cacheRecoverableSnapshot(toMsgId, sourceRecoverableSnapshot);
    }

    if (sourcePending != null && sourcePending.isNotEmpty) {
      final targetPending = _orderedPendingChunks.putIfAbsent(
        toMsgId,
        () => SplayTreeMap<int, String>(),
      );
      for (final entry in sourcePending.entries) {
        targetPending.putIfAbsent(entry.key, () => entry.value);
      }
      _drainOrderedChunks(toMsgId);
    }

    if (sourceNextChunkSeq != null && sourceNextChunkSeq > 0) {
      final currentNextChunkSeq = _nextChunkSeqByMsg[toMsgId];
      if (currentNextChunkSeq == null ||
          sourceNextChunkSeq < currentNextChunkSeq) {
        _nextChunkSeqByMsg[toMsgId] = sourceNextChunkSeq;
      }
    }

    if (sourceStream != null &&
        !identical(sourceStream, targetStream) &&
        !sourceStream.isClosed) {
      sourceStream.close();
    }
  }

  @visibleForTesting
  static void resetForTest() {
    for (final timer in _flushTimers.values) {
      timer.cancel();
    }
    _flushTimers.clear();
    _orderedPendingChunks.clear();
    _nextChunkSeqByMsg.clear();
    _buffers.clear();
    _recoverableSnapshots.clear();
    for (final stream in _streams.values) {
      if (!stream.isClosed) {
        stream.close();
      }
    }
    _streams.clear();
  }
}

class MessageBubble extends StatefulWidget {
  /// Messages above the Markdown pipeline's render limit stay collapsed in the
  /// chat list. Rendering a single megabyte-scale Text widget can monopolize
  /// Flutter's UI thread long enough to make the whole conversation immovable.
  static int get maxInlineContentCharacters =>
      ChatMarkdownEngine.pipeline.maxRenderableCharacters;

  /// Keep enough context in the bubble without making layout proportional to a
  /// potentially unbounded message body.
  static const int longContentPreviewCharacters = 2000;

  final String msgId;
  final String initialContent;
  final bool isStreaming;
  final bool isMine;

  /// 流式期标记:该消息属于"思考过程"流,需在流式期即渲染为思考卡片。
  final bool isThinking;
  final Map<String, dynamic> messageExtra;
  final ValueChanged<String>? onStreamUpdate;
  final String? quotedMessageId;
  final MessageModel? repliedMsg;
  final bool deferMarkdownRender;
  final Duration markdownRenderDeferDuration;
  final ValueChanged<ChatMessageCardData>? onMessageCardTap;
  final ChatMessageCardActionHandler? onMessageCardAction;
  final ChatManagedInputBinding? messageCardManagedInputBinding;
  final bool Function(String approvalId)? isExecApprovalPending;
  final ChatMessageCardData? messageCardDataOverride;
  final EdgeInsetsGeometry margin;
  final BorderRadiusGeometry borderRadius;
  final Future<String?> Function()? pickRemoteDirectory;

  const MessageBubble({
    super.key,
    required this.msgId,
    this.initialContent = '',
    this.isStreaming = false,
    this.isMine = false,
    this.isThinking = false,
    this.messageExtra = const {},
    this.onStreamUpdate,
    this.quotedMessageId,
    this.repliedMsg,
    this.deferMarkdownRender = false,
    this.markdownRenderDeferDuration = const Duration(milliseconds: 200),
    this.onMessageCardTap,
    this.onMessageCardAction,
    this.messageCardManagedInputBinding,
    this.isExecApprovalPending,
    this.messageCardDataOverride,
    this.margin = const EdgeInsets.symmetric(vertical: 4, horizontal: 8),
    this.borderRadius = const BorderRadius.all(Radius.circular(12)),
    this.pickRemoteDirectory,
  });

  static bool hasCachedFinalRenderState(String content) {
    return _MessageBubbleState.hasCachedFinalRenderState(content);
  }

  static bool isFinalRenderPrecacheEligible(String content) {
    return content.length <= maxInlineContentCharacters;
  }

  static void precacheFinalRenderStates(
    Iterable<String> contents, {
    int maxEntries = 10,
  }) {
    _MessageBubbleState.precacheFinalRenderStates(
      contents,
      maxEntries: maxEntries,
    );
  }

  static Future<void> hydrateFinalRenderStatesFromDisk(
    Iterable<String> contents, {
    int maxEntries = 10,
  }) {
    return _MessageBubbleState.hydrateFinalRenderStatesFromDisk(
      contents,
      maxEntries: maxEntries,
    );
  }

  @visibleForTesting
  static void resetFinalRenderCacheForTest() {
    _MessageBubbleState.resetFinalRenderCacheForTest();
  }

  @override
  State<MessageBubble> createState() => _MessageBubbleState();
}

class _MessageBubbleState extends State<MessageBubble> {
  static const Duration _selectionUnlockDoubleTapWindow = Duration(
    milliseconds: 400,
  );
  static final ChatMarkdownPipeline _markdownPipeline =
      ChatMarkdownEngine.pipeline;
  static const ChatMarkdownPipelineResult _emptyRenderState =
      ChatMarkdownPipelineResult(
        originalText: '',
        normalizedText: '',
        shouldUseMarkdown: false,
      );
  static const int _maxCachedFinalRenderStates = 64;
  static const int _maxPersistedFinalRenderStates = 1024;
  static final LinkedHashMap<String, ChatMarkdownPipelineResult>
  _finalRenderStateCache = LinkedHashMap<String, ChatMarkdownPipelineResult>();

  BehaviorSubject<String>? _stream;
  StreamSubscription? _subscription;
  ChatMarkdownPipelineResult _renderState = _emptyRenderState;
  bool _streamFinished = true;
  bool _streamOriginated = false;
  String _latestStreamPayloadText = '';
  bool _selectionActive =
      kIsWeb ||
      defaultTargetPlatform == TargetPlatform.macOS ||
      defaultTargetPlatform == TargetPlatform.windows ||
      defaultTargetPlatform == TargetPlatform.linux;
  Timer? _deferredRenderTimer;
  DateTime? _lastTapTime;
  int _deferredRenderToken = 0;
  String _fullRenderContent = '';
  bool _isInlineContentTruncated = false;
  bool _isDispatchResult = false;

  MessageModel? _repliedMsg;
  bool _isLoadingReply = false;

  static const TextHeightBehavior _plainTextHeightBehavior = TextHeightBehavior(
    applyHeightToFirstAscent: true,
    applyHeightToLastDescent: true,
  );
  static const Color _bubbleBackgroundColor = Color(0xFFFFFFFF);
  static const Color _bubbleBorderColor = Color(0xFFD9D9D9);

  @override
  void initState() {
    super.initState();
    _streamFinished = !widget.isStreaming;
    _streamOriginated = widget.isStreaming;
    final initialContent = _stripAttachmentMarkdown(
      _resolveStableWidgetContent(
        widget.initialContent,
        preferCurrentRenderIfEmpty: true,
      ),
    );
    if (_streamFinished) {
      _applySettledRenderState(initialContent);
    } else {
      _applyRenderState(initialContent, allowMarkdownRender: false);
    }
    _bindStreamIfNeeded();

    _repliedMsg = widget.repliedMsg;
    _loadReplyMsgIfNeeded();
  }

  void _loadReplyMsgIfNeeded() {
    if (_repliedMsg == null &&
        widget.quotedMessageId != null &&
        widget.quotedMessageId!.isNotEmpty) {
      _isLoadingReply = true;
      LocalDb.getMessageByMsgId(widget.quotedMessageId!).then((data) {
        if (mounted && data != null) {
          setState(() {
            _repliedMsg = MessageModel.fromJson(data);
            _isLoadingReply = false;
          });
        } else if (mounted) {
          setState(() {
            _isLoadingReply = false;
          });
        }
      });
    }
  }

  @override
  void didUpdateWidget(covariant MessageBubble oldWidget) {
    super.didUpdateWidget(oldWidget);
    final shouldRebind =
        oldWidget.msgId != widget.msgId ||
        oldWidget.isStreaming != widget.isStreaming;
    if (oldWidget.msgId != widget.msgId) {
      _selectionActive = false;
      _lastTapTime = null;
      _streamOriginated = widget.isStreaming;
      _latestStreamPayloadText = '';
    } else if (oldWidget.isStreaming || widget.isStreaming) {
      _streamOriginated = true;
    }
    if (shouldRebind) {
      _cancelDeferredFinalRender();
      _subscription?.cancel();
      _subscription = null;
      _stream = null;
      _streamFinished = !widget.isStreaming;
      final nextContent = _stripAttachmentMarkdown(
        _resolveStableWidgetContent(
          widget.initialContent,
          preferCurrentRenderIfEmpty: oldWidget.msgId == widget.msgId,
        ),
      );
      if (_streamFinished) {
        _applySettledRenderState(nextContent);
      } else {
        _applyRenderState(nextContent, allowMarkdownRender: false);
      }
      _bindStreamIfNeeded();
    }
    if (oldWidget.quotedMessageId != widget.quotedMessageId ||
        oldWidget.repliedMsg != widget.repliedMsg) {
      if (widget.repliedMsg != null) {
        _repliedMsg = widget.repliedMsg;
      } else if (oldWidget.quotedMessageId != widget.quotedMessageId) {
        _repliedMsg = null;
        _loadReplyMsgIfNeeded();
      }
    }
    if (shouldRebind) return;

    if (!widget.isStreaming &&
        oldWidget.initialContent != widget.initialContent) {
      _streamFinished = true;
      final nextContent = _stripAttachmentMarkdown(
        _resolveStableWidgetContent(
          widget.initialContent,
          preferCurrentRenderIfEmpty: true,
        ),
      );
      _applySettledRenderState(nextContent);
      return;
    }

    if (!widget.isStreaming &&
        oldWidget.deferMarkdownRender &&
        !widget.deferMarkdownRender) {
      _streamFinished = true;
      _applyRenderState(
        _stripAttachmentMarkdown(
          _resolveStableWidgetContent(
            widget.initialContent,
            preferCurrentRenderIfEmpty: true,
          ),
        ),
        allowMarkdownRender: true,
      );
    }

    // When messageExtra changes (e.g. server ack populates attachments),
    // recompute render state to strip attachment markdown from text area.
    if (!widget.isStreaming &&
        !identical(widget.messageExtra, oldWidget.messageExtra)) {
      _applySettledRenderState(
        _stripAttachmentMarkdown(
          _resolveStableWidgetContent(
            widget.initialContent,
            preferCurrentRenderIfEmpty: true,
          ),
        ),
      );
    }
  }

  /// Strips generated attachment markdown (e.g. ![image](<url>)) from [content]
  /// when the message has structured attachments in [widget.messageExtra].
  /// Prevents duplicate rendering in both the attachment grid and inline text.
  String _stripAttachmentMarkdown(String content) {
    final attachments = ChatMessageAttachmentCodec.readFromExtra(
      widget.messageExtra,
    );
    if (attachments.isEmpty) return content;
    return ChatMessageAttachmentCodec.stripGeneratedAttachmentContent(
      content,
      attachments,
    );
  }

  String _resolveStableWidgetContent(
    String incomingContent, {
    required bool preferCurrentRenderIfEmpty,
  }) {
    if (!preferCurrentRenderIfEmpty) {
      return incomingContent;
    }
    if (incomingContent.trim().isNotEmpty) {
      return incomingContent;
    }
    final visibleSnapshot = _resolveVisibleSnapshotContent();
    if (visibleSnapshot.isNotEmpty) {
      return visibleSnapshot;
    }
    return incomingContent;
  }

  String _resolveVisibleSnapshotContent() {
    if (_latestStreamPayloadText.trim().isNotEmpty) {
      return _latestStreamPayloadText;
    }
    if (_renderState.originalText.trim().isNotEmpty) {
      return _renderState.originalText;
    }
    if (_renderState.normalizedText.trim().isNotEmpty) {
      return _renderState.normalizedText;
    }
    // Last resort: check the stream controller buffer directly.
    // Prevents content loss during stream finish → widget rebuild gaps.
    final peeked = MessageStreamController.peekRecoverableContent(widget.msgId);
    if (peeked.trim().isNotEmpty) {
      return ChatMessageContent.unwrapStructuredText(peeked);
    }
    return '';
  }

  void _applyDeferredOrCachedFinalRender(String data) {
    _cancelDeferredFinalRender();
    if (_shouldUseTrustedStreamFinalRender()) {
      _applyRenderState(data, allowMarkdownRender: true);
      return;
    }
    if (!MessageBubble.isFinalRenderPrecacheEligible(data)) {
      _applyRenderState(data, allowMarkdownRender: false);
      return;
    }
    final structured = ChatMessageContent.unwrapStructuredText(data);
    _isDispatchResult = ChatMessageContent.isDispatchResultMessage(structured);
    final cached = _takeCachedFinalRenderState(_normalizeCacheInput(data));
    if (cached != null) {
      _fullRenderContent = _normalizeCacheInput(data);
      _renderState = _forceMarkdownIfNeeded(
        cached,
        forceMarkdown: _isDispatchResult,
      );
      return;
    }
    _applyRenderState(data, allowMarkdownRender: false);
    _scheduleDeferredFinalRender(data);
  }

  void _applySettledRenderState(String data) {
    if (widget.deferMarkdownRender && !_shouldUseTrustedStreamFinalRender()) {
      _applyDeferredOrCachedFinalRender(data);
      return;
    }
    _applyRenderState(data, allowMarkdownRender: true);
  }

  void _bindStreamIfNeeded() {
    if (!widget.isStreaming) return;
    _streamFinished = false;
    _stream = MessageStreamController.getStream(widget.msgId);
    if (widget.initialContent.isNotEmpty &&
        _stream!.value.isEmpty &&
        !MessageStreamController.hasActiveProducer(widget.msgId)) {
      _stream!.add(widget.initialContent);
    }
    _subscription = _stream!.listen(
      (data) {
        if (!mounted) return;
        final normalizedData = ChatMessageContent.unwrapStructuredText(data);
        final previewSource = normalizedData.isEmpty
            ? _resolveVisibleSnapshotContent()
            : normalizedData;
        final preview = _buildStreamingPlainTextState(previewSource);
        if (preview.normalizedText == _renderState.normalizedText) return;
        setState(() {
          if (normalizedData.isNotEmpty) {
            _latestStreamPayloadText = normalizedData;
          }
          _renderState = preview;
        });
        widget.onStreamUpdate?.call(widget.msgId);
      },
      onDone: () {
        if (!mounted) return;
        _cancelDeferredFinalRender();
        setState(() {
          _streamFinished = true;
          _applyRenderState(
            _resolveAuthoritativeStreamFinalContent(),
            allowMarkdownRender: true,
          );
        });
      },
    );
    // Defensive: if BehaviorSubject replayed empty seed but buffer has content,
    // populate _renderState now so first build() isn't blank.
    if (_renderState.normalizedText.isEmpty) {
      final peeked = MessageStreamController.peekRecoverableContent(
        widget.msgId,
      );
      if (peeked.isNotEmpty) {
        final normalized = ChatMessageContent.unwrapStructuredText(peeked);
        if (normalized.isNotEmpty) {
          _renderState = _buildStreamingPlainTextState(normalized);
        }
      }
    }
  }

  void _applyRenderState(String data, {required bool allowMarkdownRender}) {
    // Oversized malformed JSON-like tool output is common in agent sessions.
    // Do not feed it through jsonDecode before truncation: a leading `[` is
    // enough for ChatMessageContent to attempt a full parse.
    final oversizedInput = !MessageBubble.isFinalRenderPrecacheEligible(data);
    final structured = oversizedInput
        ? data
        : ChatMessageContent.unwrapStructuredText(data);
    _isDispatchResult = ChatMessageContent.isDispatchResultMessage(structured);
    final normalizedInput = oversizedInput
        ? ChatMessageContent.unwrapDispatchResult(data)
        : _normalizeCacheInput(data);
    _fullRenderContent = normalizedInput;
    _isInlineContentTruncated =
        oversizedInput ||
        normalizedInput.length > MessageBubble.maxInlineContentCharacters;
    final inlineInput = _isInlineContentTruncated
        ? _buildLongContentPreview(normalizedInput)
        : normalizedInput;
    _renderState = _buildRenderState(
      inlineInput,
      allowMarkdownRender: allowMarkdownRender && !_isInlineContentTruncated,
      forceMarkdown: _isDispatchResult,
    );
  }

  ChatMarkdownPipelineResult _buildRenderState(
    String data, {
    required bool allowMarkdownRender,
    bool forceMarkdown = false,
  }) {
    final normalizedInput = _normalizeCacheInput(data);
    if (!allowMarkdownRender) {
      if (widget.isStreaming && !_streamFinished) {
        return _buildStreamingPlainTextState(normalizedInput);
      }
      return _markdownPipeline.preparePreview(normalizedInput);
    }
    final cached = _takeCachedFinalRenderState(normalizedInput);
    if (cached != null) {
      return _forceMarkdownIfNeeded(cached, forceMarkdown: forceMarkdown);
    }
    if (_shouldUseTrustedStreamFinalRender()) {
      return _forceMarkdownIfNeeded(
        _markdownPipeline.prepareFinalRenderFromTrustedSource(normalizedInput),
        forceMarkdown: forceMarkdown,
      );
    }
    final parsed = forceMarkdown
        ? _markdownPipeline.prepareFinalRenderFromTrustedSource(
            normalizedInput,
          )
        : _markdownPipeline.prepareFinalRender(normalizedInput);
    final result = _forceMarkdownIfNeeded(
      parsed,
      forceMarkdown: forceMarkdown,
    );
    _cacheAndPersistFinalRenderState(normalizedInput, result);
    return result;
  }

  ChatMarkdownPipelineResult _forceMarkdownIfNeeded(
    ChatMarkdownPipelineResult state, {
    required bool forceMarkdown,
  }) {
    if (!forceMarkdown ||
        state.shouldUseMarkdown ||
        state.document == null ||
        state.normalizedText.trim().isEmpty) {
      return state;
    }
    return ChatMarkdownPipelineResult(
      originalText: state.originalText,
      normalizedText: state.normalizedText,
      shouldUseMarkdown: true,
      document: state.document,
      semantics: state.semantics,
      validation: state.validation,
    );
  }

  bool _shouldUseTrustedStreamFinalRender() {
    return _streamOriginated && _streamFinished;
  }

  String _resolveAuthoritativeStreamFinalContent() {
    if (_latestStreamPayloadText.trim().isNotEmpty) {
      return _latestStreamPayloadText;
    }
    return _resolveVisibleSnapshotContent();
  }

  void _scheduleDeferredFinalRender(String data) {
    if (!widget.deferMarkdownRender || widget.isStreaming) {
      return;
    }
    _cancelDeferredFinalRender();
    final token = ++_deferredRenderToken;
    _deferredRenderTimer = Timer(widget.markdownRenderDeferDuration, () {
      if (!mounted || token != _deferredRenderToken) {
        return;
      }
      final nextState = _buildRenderState(data, allowMarkdownRender: true);
      final unchanged =
          nextState.normalizedText == _renderState.normalizedText &&
          nextState.shouldUseMarkdown == _renderState.shouldUseMarkdown &&
          nextState.document == _renderState.document &&
          nextState.semantics == _renderState.semantics;
      if (unchanged) {
        return;
      }
      setState(() {
        _renderState = nextState;
      });
    });
  }

  void _cancelDeferredFinalRender() {
    _deferredRenderToken += 1;
    _deferredRenderTimer?.cancel();
    _deferredRenderTimer = null;
  }

  static ChatMarkdownPipelineResult? _takeCachedFinalRenderState(
    String normalizedInput,
  ) {
    final cached = _finalRenderStateCache.remove(normalizedInput);
    if (cached == null) {
      return null;
    }
    _finalRenderStateCache[normalizedInput] = cached;
    return cached;
  }

  static void _cacheFinalRenderState(
    String normalizedInput,
    ChatMarkdownPipelineResult state,
  ) {
    _finalRenderStateCache[normalizedInput] = state;
    while (_finalRenderStateCache.length > _maxCachedFinalRenderStates) {
      _finalRenderStateCache.remove(_finalRenderStateCache.keys.first);
    }
  }

  static void _cacheAndPersistFinalRenderState(
    String normalizedInput,
    ChatMarkdownPipelineResult state,
  ) {
    _cacheFinalRenderState(normalizedInput, state);
    _persistFinalRenderState(normalizedInput, state);
  }

  static void _persistFinalRenderState(
    String normalizedInput,
    ChatMarkdownPipelineResult state,
  ) {
    final record = MarkdownRenderCacheRecord(
      cacheKey: _persistentCacheKeyForNormalizedInput(normalizedInput),
      normalizedText: normalizedInput,
      payload: ChatMarkdownRenderCacheCodec.encode(state),
    );
    unawaited(
      LocalDb.upsertMarkdownRenderCaches(<MarkdownRenderCacheRecord>[
        record,
      ], maxEntries: _maxPersistedFinalRenderStates),
    );
  }

  static bool hasCachedFinalRenderState(String content) {
    if (!MessageBubble.isFinalRenderPrecacheEligible(content)) {
      return false;
    }
    final normalizedInput = _normalizeCacheInput(content);
    if (normalizedInput.isEmpty) {
      return false;
    }
    return _finalRenderStateCache.containsKey(normalizedInput);
  }

  static void precacheFinalRenderStates(
    Iterable<String> contents, {
    int maxEntries = 10,
  }) {
    if (maxEntries <= 0) {
      return;
    }

    var warmed = 0;
    for (final content in contents) {
      if (warmed >= maxEntries) {
        break;
      }
      if (!MessageBubble.isFinalRenderPrecacheEligible(content)) {
        continue;
      }
      final normalizedInput = _normalizeCacheInput(content);
      if (normalizedInput.isEmpty) {
        continue;
      }
      final cached = _takeCachedFinalRenderState(normalizedInput);
      if (cached != null) {
        warmed += 1;
        continue;
      }
      final parsed = _markdownPipeline.prepareFinalRender(normalizedInput);
      _cacheAndPersistFinalRenderState(normalizedInput, parsed);
      warmed += 1;
    }
  }

  static Future<void> hydrateFinalRenderStatesFromDisk(
    Iterable<String> contents, {
    int maxEntries = 10,
  }) async {
    if (maxEntries <= 0) {
      return;
    }

    final normalizedInputs = <String>[];
    final seen = <String>{};
    for (final content in contents) {
      if (normalizedInputs.length >= maxEntries) {
        break;
      }
      if (!MessageBubble.isFinalRenderPrecacheEligible(content)) {
        continue;
      }
      final normalizedInput = _normalizeCacheInput(content);
      if (normalizedInput.isEmpty) {
        continue;
      }
      if (!seen.add(normalizedInput)) {
        continue;
      }
      if (_finalRenderStateCache.containsKey(normalizedInput)) {
        continue;
      }
      normalizedInputs.add(normalizedInput);
    }
    if (normalizedInputs.isEmpty) {
      return;
    }

    final lookup = <String, String>{};
    for (final normalizedInput in normalizedInputs) {
      lookup[_persistentCacheKeyForNormalizedInput(normalizedInput)] =
          normalizedInput;
    }

    final records = await LocalDb.getMarkdownRenderCachesByKeys(
      lookup.keys.toList(growable: false),
    );
    if (records.isEmpty) {
      return;
    }

    for (final record in records) {
      final expectedNormalized = lookup[record.cacheKey];
      if (expectedNormalized == null) {
        continue;
      }
      if (record.normalizedText != expectedNormalized) {
        continue;
      }
      final decoded = ChatMarkdownRenderCacheCodec.decode(record.payload);
      if (decoded == null) {
        continue;
      }
      if (decoded.normalizedText != expectedNormalized) {
        continue;
      }
      _cacheFinalRenderState(expectedNormalized, decoded);
    }
  }

  static String _normalizeCacheInput(String data) {
    final structured = ChatMessageContent.unwrapStructuredText(data);
    return ChatMessageContent.unwrapDispatchResult(structured);
  }

  static String _buildLongContentPreview(String content) {
    if (content.length <= MessageBubble.longContentPreviewCharacters) {
      return content;
    }
    var end = MessageBubble.longContentPreviewCharacters;
    if (_isHighSurrogate(content.codeUnitAt(end - 1))) {
      end -= 1;
    }
    return '${content.substring(0, end)}\n…';
  }

  static bool _isHighSurrogate(int codeUnit) {
    return codeUnit >= 0xD800 && codeUnit <= 0xDBFF;
  }

  static String _persistentCacheKeyForNormalizedInput(String normalizedInput) {
    return sha256.convert(utf8.encode(normalizedInput)).toString();
  }

  @visibleForTesting
  static void resetFinalRenderCacheForTest() {
    _finalRenderStateCache.clear();
  }

  @override
  void dispose() {
    _cancelDeferredFinalRender();
    _subscription?.cancel();
    super.dispose();
  }

  Widget _buildReplyPreview(
    BuildContext context,
    ThemeData theme,
    Color textColor,
    double fontScale,
  ) {
    if (_repliedMsg == null) {
      if (_isLoadingReply) {
        return Padding(
          padding: const EdgeInsets.only(bottom: 8.0),
          child: Text(
            'common_loading'.tr,
            style: TextStyle(
              fontSize: 12 * fontScale,
              color: textColor.withValues(alpha: 0.6),
            ),
          ),
        );
      }
      return const SizedBox.shrink();
    }

    final content = ChatMessagePreview.summarize(_repliedMsg!.content);

    return Container(
      margin: const EdgeInsets.only(bottom: 8),
      padding: const EdgeInsets.only(left: 8, top: 2, bottom: 2),
      decoration: BoxDecoration(
        border: Border(
          left: BorderSide(color: textColor.withValues(alpha: 0.3), width: 3),
        ),
      ),
      child: Text(
        content,
        style: TextStyle(
          fontSize: 12 * fontScale,
          color: textColor.withValues(alpha: 0.7),
        ),
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final bgColor = _resolveBubbleBackgroundColor(theme);
    final textColor = _resolveBubbleTextColor(bgColor);
    final renderMarkdown =
        _renderState.shouldUseMarkdown &&
        (!widget.isStreaming || _streamFinished);
    final chatFontSizeService = Get.isRegistered<ChatFontSizeService>()
        ? Get.find<ChatFontSizeService>()
        : null;

    if (chatFontSizeService == null) {
      return _buildBubble(
        context: context,
        theme: theme,
        textColor: textColor,
        renderMarkdown: renderMarkdown,
        fontScale: 1.0,
      );
    }

    return Obx(
      () => _buildBubble(
        context: context,
        theme: theme,
        textColor: textColor,
        renderMarkdown: renderMarkdown,
        fontScale: chatFontSizeService.scaleRx.value,
      ),
    );
  }

  Widget _buildBubble({
    required BuildContext context,
    required ThemeData theme,
    required Color textColor,
    required bool renderMarkdown,
    required double fontScale,
  }) {
    final viewportWidth = MediaQuery.sizeOf(context).width;
    final attachments = ChatMessageAttachmentCodec.readFromExtra(
      widget.messageExtra,
    );
    final strippedAttachmentContent =
        ChatMessageAttachmentCodec.stripGeneratedAttachmentContent(
          widget.initialContent,
          attachments,
        );
    final structuredContent = ChatMessageContent.unwrapStructuredText(
      strippedAttachmentContent,
    );
    final dispatchResult = ChatMessageContent.tryParseDispatchResult(
      structuredContent,
    );
    final card = widget.isStreaming
        ? null
        : widget.messageCardDataOverride ??
              ChatMessageCardCodec.decodeFromMessage(
                content: strippedAttachmentContent,
              );
    // Build-time recovery: if streaming and _renderState is empty, peek at the
    // buffer before deciding to show the pending indicator.
    var effectiveRenderState = _renderState;
    if (widget.isStreaming &&
        !_streamFinished &&
        _renderState.normalizedText.isEmpty) {
      final peeked = MessageStreamController.peekRecoverableContent(
        widget.msgId,
      );
      if (peeked.isNotEmpty) {
        final normalized = ChatMessageContent.unwrapStructuredText(peeked);
        if (normalized.isNotEmpty) {
          effectiveRenderState = _buildStreamingPlainTextState(normalized);
        }
      }
    }
    final showPendingIndicator =
        widget.isStreaming &&
        !_streamFinished &&
        effectiveRenderState.normalizedText.isEmpty;
    // 流式思考:thinking 流在流式期(buffer 非空)即用思考卡片渲染实时内容;
    // buffer 为空仍走 pending 指示器,避免空卡片。finalize 后走 card 解码分支,无缝衔接。
    final streamingThinking =
        widget.isStreaming &&
        widget.isThinking &&
        !_streamFinished &&
        effectiveRenderState.normalizedText.trim().isNotEmpty;
    // When the message contains only a self-contained card (no quoted message,
    // no attachments), skip the bubble background so the card floats directly
    // without the "box within a box" look. The streaming thinking card floats
    // the same way as its finalized card form.
    final hasQuotedMessage =
        widget.quotedMessageId != null && widget.quotedMessageId!.isNotEmpty;
    final isCardOnlyBubble =
        !hasQuotedMessage &&
        attachments.isEmpty &&
        ((card != null && card.type != ChatMessageCardType.conversation) ||
            streamingThinking);
    final shouldRenderContent =
        card != null ||
        (card == null &&
            (strippedAttachmentContent.trim().isNotEmpty ||
                attachments.isEmpty));
    final plainTextStyle = AppTheme.applyTextFont(
      TextStyle(color: textColor, fontSize: 14 * fontScale, height: 1.42),
    );
    final plainTextStrutStyle = StrutStyle.fromTextStyle(
      plainTextStyle,
      height: plainTextStyle.height,
      forceStrutHeight: true,
    );
    final isDispatchResultBubble =
        dispatchResult != null ||
        _isDispatchResult ||
        ChatMessageContent.isDispatchResultMessage(structuredContent);
    // Success green: dispatch-result means a completed review/callback.
    final dispatchAccent = AppTheme.successColor;
    final bubbleBackgroundColor = isCardOnlyBubble
        ? Colors.transparent
        : isDispatchResultBubble
        ? dispatchAccent.withValues(alpha: 0.08)
        : _bubbleBackgroundColor;
    final bubbleBorderColor = isCardOnlyBubble
        ? Colors.transparent
        : isDispatchResultBubble
        ? dispatchAccent.withValues(alpha: 0.22)
        : _bubbleBorderColor;

    return RepaintBoundary(
      child: Align(
        alignment: widget.isMine ? Alignment.centerRight : Alignment.centerLeft,
        widthFactor: 1,
        child: Container(
          key: isDispatchResultBubble
              ? const Key('chat_dispatch_result_bubble')
              : null,
          margin: widget.margin,
          padding: isCardOnlyBubble
              ? EdgeInsets.zero
              : const EdgeInsets.only(left: 12, top: 12, right: 12, bottom: 12),
          clipBehavior: Clip.hardEdge,
          decoration: BoxDecoration(
            color: bubbleBackgroundColor,
            borderRadius: widget.borderRadius,
            border: Border.all(
              color: bubbleBorderColor,
              width: isCardOnlyBubble ? 0 : 1,
            ),
          ),
          constraints: BoxConstraints(maxWidth: viewportWidth * 0.8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              if (widget.quotedMessageId != null &&
                  widget.quotedMessageId!.isNotEmpty)
                _buildReplyPreview(context, theme, textColor, fontScale),
              if (showPendingIndicator)
                StreamPendingIndicator(color: textColor.withValues(alpha: 0.72))
              else if (shouldRenderContent)
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    if (card != null)
                      ChatMessageCardView(
                        card: card,
                        sourceMessageId: widget.msgId,
                        isMine: widget.isMine,
                        fontScale: fontScale,
                        onTap: widget.onMessageCardTap == null
                            ? null
                            : () => widget.onMessageCardTap!(card),
                        onAction: widget.onMessageCardAction,
                        managedInputBinding:
                            widget.messageCardManagedInputBinding,
                        isExecApprovalPending: widget.isExecApprovalPending,
                        pickRemoteDirectory: widget.pickRemoteDirectory,
                      ),
                    if (card == null)
                      dispatchResult != null
                          ? _buildSelectionUnlockListener(
                              child: ChatSelectionArea(
                                enabled: _selectionActive,
                                onSelectionCleared: _handleSelectionCleared,
                                child: ChatDispatchResultCard(
                                  result: dispatchResult,
                                  fontScale: fontScale,
                                ),
                              ),
                            )
                          : streamingThinking
                          ? ChatThinkingCardView(
                              card: ChatThinkingCardData(
                                content: effectiveRenderState.normalizedText,
                              ),
                              isMine: widget.isMine,
                              fontScale: fontScale,
                            )
                          : _buildTextContent(
                              textColor: textColor,
                              fontScale: fontScale,
                              renderState: effectiveRenderState,
                              plainTextStyle: plainTextStyle,
                              plainTextStrutStyle: plainTextStrutStyle,
                              renderMarkdown: renderMarkdown,
                            ),
                    if (card == null && _isInlineContentTruncated)
                      Align(
                        alignment: Alignment.centerLeft,
                        child: TextButton.icon(
                          key: ValueKey(
                            'chat_long_message_open_${widget.msgId}',
                          ),
                          onPressed: _showFullMessage,
                          icon: const Icon(
                            Icons.open_in_full_rounded,
                            size: 16,
                          ),
                          label: Text('common_expand'.tr),
                        ),
                      ),
                  ],
                ),
              if (attachments.isNotEmpty && shouldRenderContent)
                const SizedBox(height: 8),
              if (attachments.isNotEmpty)
                ChatMessageAttachmentGrid(attachments: attachments),
            ],
          ),
        ),
      ),
    );
  }

  Widget _buildTextContent({
    required Color textColor,
    required double fontScale,
    required ChatMarkdownPipelineResult renderState,
    required TextStyle plainTextStyle,
    required StrutStyle plainTextStrutStyle,
    required bool renderMarkdown,
  }) {
    if (renderMarkdown) {
      return _buildSelectionUnlockListener(
        child: ChatMarkdownView(
          data: renderState.normalizedText,
          textColor: textColor,
          isMine: false,
          fontScale: fontScale,
          document: renderState.document,
          semantics: renderState.semantics,
          onMessageCardAction: widget.onMessageCardAction,
          onMessageCardTap: widget.onMessageCardTap,
          sourceMessageId: widget.msgId,
          managedInputBinding: widget.messageCardManagedInputBinding,
          isExecApprovalPending: widget.isExecApprovalPending,
          pickRemoteDirectory: widget.pickRemoteDirectory,
          selectionEnabled: _selectionActive,
          onSelectionCleared: _handleSelectionCleared,
        ),
      );
    }

    // 流式阶段用轻量 Text，避免可选区组件在 Flutter Web 上每帧
    // 重建手势识别树和 SelectionArea 导致 JS 主线程卡顿。
    // 流结束后再切回 SelectionArea 供用户选择、复制。
    if (!_streamFinished) {
      return Text(
        renderState.normalizedText,
        style: plainTextStyle,
        strutStyle: plainTextStrutStyle,
        textHeightBehavior: _plainTextHeightBehavior,
      );
    }

    return _buildSelectionUnlockListener(
      child: ChatSelectionArea(
        enabled: _selectionActive,
        onSelectionCleared: _handleSelectionCleared,
        child: Text(
          renderState.normalizedText,
          style: plainTextStyle,
          strutStyle: plainTextStrutStyle,
          textHeightBehavior: _plainTextHeightBehavior,
        ),
      ),
    );
  }

  Widget _buildSelectionUnlockListener({required Widget child}) {
    return Listener(
      onPointerDown: _handleSelectionUnlockPointerDown,
      child: child,
    );
  }

  Future<void> _showFullMessage() async {
    final content = _fullRenderContent;
    if (!mounted || content.isEmpty) {
      return;
    }
    await showAppDialog<void>(
      context: context,
      builder: (context) => _LongMessageViewer(content: content),
    );
  }

  void _handleSelectionUnlockPointerDown(PointerDownEvent _) {
    final now = DateTime.now();
    final last = _lastTapTime;
    _lastTapTime = now;
    if (last == null ||
        now.difference(last) >= _selectionUnlockDoubleTapWindow ||
        _selectionActive) {
      return;
    }
    setState(() {
      _selectionActive = true;
    });
  }

  void _handleSelectionCleared() {
    if (!_selectionActive || !mounted) {
      return;
    }
    setState(() {
      _selectionActive = false;
    });
  }

  ChatMarkdownPipelineResult _buildStreamingPlainTextState(String data) {
    final normalizedInput = _normalizeCacheInput(data);
    return ChatMarkdownPipelineResult(
      originalText: normalizedInput,
      normalizedText: normalizedInput,
      shouldUseMarkdown: false,
    );
  }

  Color _resolveBubbleBackgroundColor(ThemeData theme) {
    return _bubbleBackgroundColor;
  }

  Color _resolveBubbleTextColor(Color backgroundColor) {
    return AppTheme.readableTextColorForBackground(backgroundColor);
  }
}

class _LongMessageViewer extends StatefulWidget {
  const _LongMessageViewer({required this.content});

  final String content;

  @override
  State<_LongMessageViewer> createState() => _LongMessageViewerState();
}

class _LongMessageViewerState extends State<_LongMessageViewer> {
  static const int _chunkCharacters = 8192;

  late final List<_LongMessageChunkRange> _chunks = _buildChunkRanges(
    widget.content,
  );
  bool _copied = false;

  static List<_LongMessageChunkRange> _buildChunkRanges(String content) {
    if (content.isEmpty) {
      return const <_LongMessageChunkRange>[];
    }
    final chunks = <_LongMessageChunkRange>[];
    var start = 0;
    while (start < content.length) {
      var end = start + _chunkCharacters;
      if (end > content.length) {
        end = content.length;
      }
      if (end < content.length &&
          _MessageBubbleState._isHighSurrogate(content.codeUnitAt(end - 1))) {
        end -= 1;
      }
      chunks.add(_LongMessageChunkRange(start, end));
      start = end;
    }
    return chunks;
  }

  Future<void> _copyAll() async {
    await Clipboard.setData(ClipboardData(text: widget.content));
    if (!mounted) {
      return;
    }
    setState(() {
      _copied = true;
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final viewport = MediaQuery.sizeOf(context);
    final viewerHeight = (viewport.height * 0.72)
        .clamp(180.0, 720.0)
        .toDouble();

    return AlertDialog(
      key: const ValueKey('chat_long_message_viewer'),
      title: Text('common_expand'.tr),
      content: SizedBox(
        width: 720,
        height: viewerHeight,
        child: ListView.builder(
          itemCount: _chunks.length,
          itemBuilder: (context, index) {
            final chunk = _chunks[index];
            return SelectableText(
              widget.content.substring(chunk.start, chunk.end),
              key: ValueKey('chat_long_message_chunk_$index'),
              style: AppTheme.applyTextFont(
                theme.textTheme.bodyMedium?.copyWith(height: 1.42) ??
                    const TextStyle(height: 1.42),
              ),
            );
          },
        ),
      ),
      actions: [
        TextButton.icon(
          onPressed: _copyAll,
          icon: Icon(_copied ? Icons.check_rounded : Icons.copy_rounded),
          label: Text('common_copy'.tr),
        ),
        TextButton(
          key: const ValueKey('chat_long_message_close'),
          onPressed: () => Navigator.of(context).pop(),
          child: Text('common_close'.tr),
        ),
      ],
    );
  }
}

class _LongMessageChunkRange {
  const _LongMessageChunkRange(this.start, this.end);

  final int start;
  final int end;
}
