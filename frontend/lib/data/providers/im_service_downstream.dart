part of 'im_service.dart';

extension _ImServiceDownstream on ImService {
  /// Single-entry point for all inbound WS payloads.
  /// Parses JSON exactly once, then dispatches to immediate handlers or the
  /// async downstream queue.
  void _handleSocketPayload(String payloadStr) {
    String cmd = '';
    Map<String, dynamic>? decoded;
    try {
      final raw = jsonDecode(payloadStr);
      if (raw is Map) {
        decoded = Map<String, dynamic>.from(raw);
        cmd = decoded['cmd']?.toString() ?? '';
      }
    } catch (_) {}

    // -- Immediate handlers (must not wait for async queue) --

    if (cmd == 'pong') {
      _recordPongReceipt();
      return;
    }

    // Call signaling must not wait behind IndexedDB/message hydration work.
    // Mobile Safari can pause or slow that queue while microphone permission
    // is being granted, but the LiveKit room token still needs immediate
    // handling.
    if (cmd.startsWith('call:') && decoded != null) {
      final payload = Map<String, dynamic>.from(
        decoded['payload'] is Map ? decoded['payload']! : {},
      );
      switch (cmd) {
        case 'call:invite_ack':
          _handleCallInviteAck(payload);
          return;
        case 'call:ring':
          _handleCallRing(payload);
          return;
        case 'call:peer_answered':
          _handleCallPeerAnswered(payload);
          return;
        case 'call:ai_delegated':
          _handleCallAiDelegated(payload);
          return;
        case 'call:listen_ack':
          _handleCallListenAck(payload);
          return;
        case 'call:voice_status_end':
          _handleCallVoiceStatusEnd(payload);
          return;
        case 'call:state':
        case 'call:timeout':
        case 'call:busy':
          _handleCallState(payload);
          return;
        case 'call:voice_delegate_ack':
          _handleCallVoiceDelegateAck(payload);
          return;
        case 'call:queued':
          _handleCallQueued(payload);
          return;
        case 'call:queue_update':
          _handleCallQueueUpdate(payload);
          return;
        case 'call:queue_expired':
          _handleCallQueueExpired(payload);
          return;
      }
    }

    // Request/response completers: pure in-memory seq matching, no DB work.
    // Must not wait behind the downstream queue — a backlog of push_msg DB
    // writes can delay these past their 15-second frontend timeout.
    if ((cmd == 'agent_file_list_resp' ||
            cmd == 'audit_get_manifest_resp' ||
            cmd == 'audit_list_spans_resp' ||
            cmd == 'audit_get_content_chunk_resp' ||
            cmd == 'agent_create_folder_resp' ||
            cmd == 'agent_session_bindings_list_resp' ||
            cmd == 'agent_skill_upload_resp' ||
            cmd == 'agent_skill_delete_resp' ||
            cmd == 'agent_skill_enable_resp' ||
            cmd == 'agent_skill_disable_resp' ||
            cmd == 'agent_skill_refresh_resp') &&
        decoded != null) {
      _handlePendingResponseImmediate(cmd, decoded);
      return;
    }

    // Immediately ack push_msg before queue processing so the server's
    // 5-second ack timer does not expire while the downstream queue is
    // blocked by auth/post-auth DB operations (slow on Web / IndexedDB).
    if (cmd == 'push_msg' && decoded != null) {
      final payload = decoded['payload'];
      if (payload is Map) {
        final msgId = payload['msg_id']?.toString() ?? '';
        if (msgId.isNotEmpty) {
          _sendPushAck(msgId);
        }
      }
    }

    // -- Enqueue for async processing (cmd already extracted, no re-parse) --
    _enqueueDownstream(payloadStr, cmd);
  }

  void _handlePendingResponseImmediate(String cmd, Map<String, dynamic> data) {
    final payload = data['payload'];
    switch (cmd) {
      case 'agent_file_list_resp':
        final respSeq = data['seq'];
        debugPrint(
          '[file-list-diag] front << recv resp rawSeq=$respSeq '
          'rawSeqType=${respSeq.runtimeType} '
          'isInt=${respSeq is int} '
          'pendingKeys=${_fileListPending.keys.toList()} '
          'payloadKeys=${payload is Map ? payload.keys.toList() : "<not_map>"}',
        );
        int? matchSeq;
        if (respSeq is int && _fileListPending.containsKey(respSeq)) {
          matchSeq = respSeq;
        } else if (respSeq is num) {
          final asInt = respSeq.toInt();
          if (_fileListPending.containsKey(asInt)) {
            matchSeq = asInt;
            debugPrint(
              '[file-list-diag] front << seq matched via num.toInt fallback '
              'raw=$respSeq -> $asInt',
            );
          }
        }
        if (matchSeq != null) {
          final completer = _fileListPending.remove(matchSeq)!;
          if (!completer.isCompleted) {
            final err = payload is Map ? payload['error']?.toString() : null;
            if (err != null && err.isNotEmpty) {
              debugPrint(
                '[file-list-diag] front << resp error seq=$matchSeq err=$err',
              );
              completer.completeError(Exception(err));
            } else {
              final files = payload is Map ? (payload['files'] as List?) : null;
              if (payload is Map) {
                final cp = payload['current_path'];
                if (cp is String && cp.isNotEmpty) {
                  _fileListCurrentPath[matchSeq] = cp;
                }
                final mn = payload['machine_name'];
                if (mn is String && mn.isNotEmpty) {
                  _fileListMachineName[matchSeq] = mn;
                }
              }
              debugPrint(
                '[file-list-diag] front << resp ok seq=$matchSeq '
                'count=${files?.length ?? 0}',
              );
              completer.complete(files?.cast<Map<String, dynamic>>() ?? []);
            }
          }
        } else {
          debugPrint(
            '[file-list-diag] front !! resp dropped: no pending match for '
            'seq=$respSeq (type=${respSeq.runtimeType})',
          );
        }
        break;

      case 'audit_get_manifest_resp':
      case 'audit_list_spans_resp':
      case 'audit_get_content_chunk_resp':
        _completeConversationAuditResponse(data['seq'], payload);
        break;

      case 'agent_create_folder_resp':
        final cfSeq = data['seq'];
        if (cfSeq is int && _createFolderPending.containsKey(cfSeq)) {
          final completer = _createFolderPending.remove(cfSeq)!;
          if (!completer.isCompleted) {
            final err = payload is Map ? payload['error']?.toString() : null;
            if (err != null && err.isNotEmpty) {
              completer.completeError(Exception(err));
            } else {
              final folder = payload is Map
                  ? (payload['folder'] as Map<String, dynamic>?)
                  : null;
              if (folder != null) {
                completer.complete(Map<String, dynamic>.from(folder));
              } else {
                completer.completeError(
                  Exception('im_create_folder_no_info'.tr),
                );
              }
            }
          }
        }
        break;

      case 'agent_session_bindings_list_resp':
        final sbSeq = data['seq'];
        if (sbSeq is int && _sessionBindingsPending.containsKey(sbSeq)) {
          final completer = _sessionBindingsPending.remove(sbSeq)!;
          if (!completer.isCompleted) {
            final err = payload is Map ? payload['error']?.toString() : null;
            if (err != null && err.isNotEmpty) {
              completer.completeError(Exception(err));
            } else {
              final bindings = payload is Map
                  ? (payload['bindings'] as List?)
                  : null;
              completer.complete(bindings?.cast<Map<String, dynamic>>() ?? []);
            }
          }
        }
        break;

      case 'agent_skill_upload_resp':
        final suSeq = data['seq'];
        if (suSeq is int && _skillUploadPending.containsKey(suSeq)) {
          final completer = _skillUploadPending.remove(suSeq)!;
          if (!completer.isCompleted) {
            final err = payload is Map ? payload['error']?.toString() : null;
            if (err != null && err.isNotEmpty) {
              completer.completeError(Exception(err));
            } else {
              completer.complete();
            }
          }
        }
        break;

      case 'agent_skill_delete_resp':
        final sdSeq = data['seq'];
        if (sdSeq is int && _skillDeletePending.containsKey(sdSeq)) {
          final completer = _skillDeletePending.remove(sdSeq)!;
          if (!completer.isCompleted) {
            final err = payload is Map ? payload['error']?.toString() : null;
            if (err != null && err.isNotEmpty) {
              completer.completeError(Exception(err));
            } else {
              completer.complete();
            }
          }
        }
        break;

      case 'agent_skill_enable_resp':
        _completeSkillLibraryActionResp(
          data['seq'],
          payload,
          _skillEnablePending,
        );
        break;

      case 'agent_skill_disable_resp':
        _completeSkillLibraryActionResp(
          data['seq'],
          payload,
          _skillDisablePending,
        );
        break;

      case 'agent_skill_refresh_resp':
        // seq 兜底与 file_list 一致：部分平台 JSON 解码成 num 子类而非 int。
        final srRawSeq = data['seq'];
        final srSeq = srRawSeq is int
            ? srRawSeq
            : srRawSeq is num
            ? srRawSeq.toInt()
            : null;
        if (srSeq != null && _skillRefreshPending.containsKey(srSeq)) {
          final completer = _skillRefreshPending.remove(srSeq)!;
          if (!completer.isCompleted) {
            final map = payload is Map
                ? Map<String, dynamic>.from(payload)
                : <String, dynamic>{};
            final err = map['error']?.toString();
            if (err != null && err.isNotEmpty) {
              completer.completeError(Exception(err));
            } else {
              // 先 remove pending 再解析：解析抛错也必须 completeError，
              // 否则 20s 超时因 pending 已删而永不触发，RefreshIndicator 永久转圈。
              try {
                // 快照同时喂给常规工具栏状态，聊天页工具条与弹窗外的其它入口同步更新。
                _applyAgentToolbarSnapshotPayload(map);
                final rawSnapshot = map['snapshot'];
                final snapshotMap = rawSnapshot is Map
                    ? Map<String, dynamic>.from(rawSnapshot)
                    : <String, dynamic>{};
                completer.complete(AgentToolbarModel.fromJson(snapshotMap));
              } catch (e) {
                completer.completeError(e);
              }
            }
          }
        }
        break;
    }
  }

  void _completeSkillLibraryActionResp(
    dynamic seq,
    dynamic payload,
    Map<int, Completer<Map<String, dynamic>>> pending,
  ) {
    if (seq is! int || !pending.containsKey(seq)) return;
    final completer = pending.remove(seq)!;
    if (completer.isCompleted) return;
    final map = payload is Map
        ? Map<String, dynamic>.from(payload)
        : <String, dynamic>{};
    final err = map['error']?.toString();
    if (err != null && err.isNotEmpty) {
      completer.completeError(Exception(err));
    } else {
      completer.complete(map);
    }
  }

  void _completeConversationAuditResponse(dynamic seq, dynamic payload) {
    final normalizedSeq = seq is int
        ? seq
        : seq is num
        ? seq.toInt()
        : null;
    if (normalizedSeq == null ||
        !_conversationAuditPending.containsKey(normalizedSeq)) {
      return;
    }
    final completer = _conversationAuditPending.remove(normalizedSeq)!;
    if (completer.isCompleted) return;
    completer.complete(
      payload is Map
          ? Map<String, dynamic>.from(payload)
          : <String, dynamic>{
              'error_code': 'AUDIT_INTERNAL',
              'error_message': 'chat_audit_detail_response_malformed'.tr,
            },
    );
  }

  void _enqueueDownstream(String payloadStr, String cmd) {
    final enqueuedAtMs = DateTime.now().millisecondsSinceEpoch;

    if (ImService._streamDownstreamCommands.contains(cmd)) {
      _streamDownstreamQueue = _streamDownstreamQueue.then<void>(
        (_) => _handleDownstream(payloadStr, enqueuedAtMs: enqueuedAtMs),
        onError: (_) =>
            _handleDownstream(payloadStr, enqueuedAtMs: enqueuedAtMs),
      );
      return;
    }
    _downstreamQueue = _downstreamQueue.then<void>(
      (_) => _handleDownstream(payloadStr, enqueuedAtMs: enqueuedAtMs),
      onError: (_) => _handleDownstream(payloadStr, enqueuedAtMs: enqueuedAtMs),
    );
  }

  Future<void> _handleDownstream(String payloadStr, {int? enqueuedAtMs}) async {
    try {
      final data = jsonDecode(payloadStr);
      final cmd = data['cmd'];
      final payload = data['payload'] ?? {};
      final queueLagMs = _resolveQueueLagMs(enqueuedAtMs);
      if (queueLagMs >= 1500) {
        _logDownstreamLag(cmd?.toString() ?? 'unknown', queueLagMs);
      }

      switch (cmd) {
        case 'auth_ack':
          _cancelAuthHandshakeTimer();
          final code = payload['code'];
          if (code == 0) {
            _resetSendAckTimeoutStreak();
            _isAuthenticated.value = true;
            _reconnectAttempts = 0;
            _setConnectionStage(ImConnectionStage.connected);
            debugPrint('✅ Auth success, user_id: ${payload['user_id']}');
            await _applyAuthAckInboxBootstrap(payload);
            await _handleAuthAckSuccess();
          } else {
            final msg = payload['msg']?.toString() ?? '';
            debugPrint('❌ Auth failed: code=$code msg=$msg');
            if (code == ImService.authCodeRetryable) {
              // 服务端明说这是它自己的临时故障（存储层不可用等），凭证没问题。
              // 保留会话继续退避重连，等它恢复后自愈。
              debugPrint('⚠️ Server temporarily unavailable, keep session');
              _allowReconnect = true;
              _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
            } else {
              // 凭证或账号的问题。交给凭证刷新裁决：刷得动说明只是 access token
              // 过期，换新的重连即可；服务端明确判定凭证无效，才清会话回登录页。
              await _handleWsCredentialFailure(reAuth: false);
            }
          }
          break;

        case 'pong':
          _recordPongReceipt();
          break;

        case 'push_msg':
          final msgDict = Map<String, dynamic>.from(payload as Map);
          final incomingInboxSeq = _toInt(msgDict['inbox_seq']);
          await _ensureInboxSeqCursorLoaded();
          final prevInboxSeq = _lastInboxSeqCursor;

          final sid = msgDict['session_id']?.toString().trim() ?? '';
          var createdAt = _toInt(msgDict['created_at']);
          if (createdAt > 0 && createdAt < 10000000000) {
            createdAt = createdAt * 1000;
          }
          if (!_shouldAcceptDeletedSessionActivity(sid, createdAt)) {
            final msgId = msgDict['msg_id']?.toString() ?? '';
            _observeInboxSeq(incomingInboxSeq);
            if (msgId.isNotEmpty) {
              _sendPushAck(msgId);
            }
            break;
          }

          final incomingMsgId = msgDict['msg_id']?.toString() ?? '';
          final incomingMsgType = _toInt(msgDict['msg_type']);
          if (incomingMsgId.isNotEmpty && incomingMsgType != 4) {
            final incomingContent = msgDict['content']?.toString() ?? '';
            final resolvedContent = _resolveStreamingFinalContent(
              msgId: incomingMsgId,
              incomingContent: incomingContent,
            );
            if (resolvedContent != incomingContent) {
              msgDict['content'] = resolvedContent;
            }
          }

          if (prevInboxSeq > 0 && incomingInboxSeq > prevInboxSeq + 1) {
            debugPrint(
              '⚠️ inbox_seq gap detected: prev=$prevInboxSeq incoming=$incomingInboxSeq',
            );
            _triggerPullSyncThrottled(cursorOverride: prevInboxSeq);
          }

          if (_toBool(msgDict['is_revoked'])) {
            final revokePersisted = await _consumeIncomingRevokedMessage(
              msgDict,
              dbOpLabel: 'deleteMessage(push_msg_revoke)',
            );
            if (revokePersisted) {
              _observeInboxSeq(incomingInboxSeq);
            } else if (prevInboxSeq > 0 && incomingInboxSeq > prevInboxSeq) {
              _triggerPullSyncAfterPersistFailure(cursorOverride: prevInboxSeq);
            } else {
              _triggerPullSyncAfterPersistFailure();
            }
            if (incomingMsgId.isNotEmpty) {
              _sendPushAck(incomingMsgId);
            }
            break;
          }

          if (_shouldSuppressIncomingMessageMap(msgDict)) {
            final msgId = msgDict['msg_id']?.toString() ?? '';
            _suppressIncomingMessage(
              msgId: msgId,
              sessionId: sid,
              reason: 'push_msg_suppressed_tool_gate',
              queueLagMs: _resolveQueueLagMs(enqueuedAtMs),
            );
            _observeInboxSeq(incomingInboxSeq);
            if (msgId.isNotEmpty) {
              _sendPushAck(msgId);
            }
            break;
          }
          final msgModel = MessageModel.fromJson(msgDict);
          if (msgModel.content.trim().isNotEmpty) {
            msgDict['content'] = msgModel.content;
          }
          if (_isDelegateOriginExtra(msgModel.extra)) {
            _markDelegateChannelHealthy(msgModel.sessionId);
          }
          if (msgModel.msgType != 4) {
            _markSessionComposingResolvedForParticipant(
              msgModel.sessionId,
              participantId: msgModel.senderId,
              participantType: _sessionActivityActorTypeFromSenderType(
                msgModel.senderType,
              ),
              resolvedAt: msgModel.createdAt,
            );
            _clearSessionComposingActivitiesForMessage(
              msgModel.sessionId,
              msgId: msgModel.msgId,
              senderId: msgModel.senderId,
              senderType: msgModel.senderType,
            );
          }
          if (msgModel.msgType != 4 && msgModel.msgId.isNotEmpty) {
            final wasStreaming = _activeStreamingMsgIds.remove(msgModel.msgId);
            if (wasStreaming) {
              _discardStreamingSessionPreview(msgModel.msgId);
              _clearStreamChunkGapTrackingForMessage(msgModel.msgId);
              _streamDiagFinalize(
                msgModel.msgId,
                reason: 'push_msg_non_stream',
                finalContent: msgModel.content,
                queueLagMs: _resolveQueueLagMs(enqueuedAtMs),
              );
              _streamingPlaceholders.remove(msgModel.msgId);
              MessageStreamController.finish(msgModel.msgId, msgModel.content);
            }
          }
          if (msgModel.msgType != 4 &&
              msgModel.senderType == 2 &&
              sid.isNotEmpty) {
            // Keep agent output capsule until explicit terminal status arrives.
            // Intermediate non-stream messages (tool cards, status cards, etc.)
            // should not clear typing/progress state.
          }
          final sessionType = _normalizeSessionTypeFromWire(
            msgDict['session_type'],
            fallback: _sessionTypeHints[msgModel.sessionId] ?? 'private',
          );
          _sessionTypeHints[msgModel.sessionId.trim()] = sessionType;
          if (_isCurrentSession(msgModel.sessionId) &&
              !ImService._dbChangeEventDrivenWindow) {
            _appendUIMessage(msgModel);
          }

          final pushPersisted = await _guardDbWrite(
            () => LocalDb.batchInsertMessages([msgDict]),
            op: 'batchInsertMessages(push_msg)',
          );
          // 仅在确实落盘成功后才推进 inbox_seq 游标。落盘失败（超时/异常）时保留
          // 旧游标，使后续 push_msg 的 gap 检测或重连 pull_sync 能用旧游标把这条
          // 重新拉回，避免它从 pull_sync 主链永久丢失。UI 此前已乐观显示，pull_sync
          // 补齐落盘后由幂等 upsert 收敛，不会重复。
          if (pushPersisted) {
            _observeInboxSeq(incomingInboxSeq);
          }
          if (msgModel.msgId.isNotEmpty && sid.isNotEmpty) {
            LocalDbChangeBus.instance.emitMessageChange(
              LocalMessagesInserted(
                sessionId: sid,
                msgIds: [msgModel.msgId],
                maxCreatedAt: msgModel.createdAt,
                rows: [msgDict],
              ),
            );
          }
          if (sid.isNotEmpty) {
            _clearSessionLocalDeleteMark(sid);
            // 私聊消息载荷带会话成员身份时直接定对端，不依赖 sender 推导，
            // 系统消息（sender_type=3）与新线程首条消息也能一次归组对。
            final pushPeer = _peerIdentityFromMessageMembers(
              msgDict['session_members'],
            );
            await _guardDbOp(
              _touchSessionByMessage(
                msgModel,
                increaseUnread:
                    !_isCurrentSession(sid) &&
                    !_isMessageFromCurrentUser(msgModel.senderId),
                peerIdHint: pushPeer.peerId,
                peerTypeHint: pushPeer.peerType,
              ),
              op: 'touchSession(push_msg)',
            );
            unawaited(
              _hydratePrivateSessionTitle(
                sessionId: sid,
                sessionType: sessionType,
                senderId: msgModel.senderId,
                senderType: msgModel.senderType,
              ),
            );
            _markCurrentSessionSeenFromSender(
              sid,
              msgModel.senderId,
              msgId: msgModel.msgId,
            );
          }
          // 非当前会话的审批卡片 → 全局横幅提醒
          // 使用与消息气泡相同的 codec 解码，确保弹窗和渲染一致
          if (!_isCurrentSession(sid) &&
              !_isMessageFromCurrentUser(msgModel.senderId) &&
              msgModel.content.contains('grix://card/exec_approval')) {
            final decodedApprovalCard = ChatMessageCardCodec.decodeFromMessage(
              content: msgModel.content,
            );
            if (decodedApprovalCard is ChatExecApprovalCardData) {
              _notifyInAppCardBanner(
                sessionId: sid,
                sessionType: sessionType,
                approvalCard: decodedApprovalCard,
              );
            }
          }

          if (msgModel.msgId.isNotEmpty) {
            _sendPushAck(msgModel.msgId);
          }
          break;

        case 'friend_event':
          final friendService = Get.isRegistered<FriendService>()
              ? Get.find<FriendService>()
              : null;
          if (friendService != null && payload is Map) {
            final event = Map<String, dynamic>.from(payload);
            friendService.applyRealtimeEvent(event);
            _updateFriendEventSeq(event['event_seq']);
          }
          break;

        case 'session_member_changed':
          if (payload is Map) {
            final sid = payload['session_id']?.toString().trim() ?? '';
            if (sid.isNotEmpty) {
              _bumpSessionMemberEventVersion(sid);
              await _handleSessionMemberChangedEvent(
                sid,
                Map<String, dynamic>.from(payload),
              );
            }
          }
          break;

        case 'session_access_revoked':
          if (payload is Map) {
            final sid = payload['session_id']?.toString().trim() ?? '';
            if (sid.isNotEmpty) {
              final reason = payload['reason']?.toString().trim() ?? 'revoked';
              _markSessionAccessRevoked(sid, reason: reason);
              await revokeSessionAccess(sid);
            }
          }
          break;

        case 'session_read_sync':
          if (payload is Map) {
            _applySessionReadSync(Map<String, dynamic>.from(payload));
          }
          break;

        case 'unread_sync':
          if (payload is Map) {
            _applyUnreadSync(Map<String, dynamic>.from(payload));
          }
          break;

        case 'friend_sync_resp':
          _friendSyncInFlight = false;
          final friendService = Get.isRegistered<FriendService>()
              ? Get.find<FriendService>()
              : null;
          final hasMore = payload['has_more'] == true;
          final events = List<dynamic>.from(payload['events'] ?? []);
          if (friendService != null) {
            for (final item in events) {
              if (item is Map) {
                final event = Map<String, dynamic>.from(item);
                friendService.applyRealtimeEvent(event);
                _updateFriendEventSeq(event['event_seq']);
              }
            }
          }
          _updateFriendEventSeq(payload['max_event_seq']);
          if (hasMore) {
            _triggerFriendSync();
          }
          break;

        case 'stream_chunk':
          final msgId = payload['msg_id']?.toString();
          final chunk = payload['delta_content']?.toString();
          final chunkSeq = _toInt(payload['chunk_seq']);
          final sid = payload['session_id']?.toString().trim() ?? '';
          final isCurrentSession = _isCurrentSession(sid);

          if (chunk != null &&
              chunk.isNotEmpty &&
              msgId != null &&
              msgId.isNotEmpty) {
            final existingMessage = _messageInCurrentWindowOrPlaceholder(msgId);
            if (existingMessage != null && existingMessage.msgType != 4) {
              break;
            }
            if (_locallyStoppedStreamMsgIds.contains(msgId)) {
              break;
            }
            if (isCurrentSession) {
              final queueLagMs = _resolveQueueLagMs(enqueuedAtMs);
              _streamDiagOnChunk(
                msgId: msgId,
                sessionId: sid,
                chunk: chunk,
                chunkSeq: chunkSeq,
                queueLagMs: queueLagMs,
              );
            }
            // 加入活跃集合并刷新活动时间：每个 chunk 到达都刷新，
            // 流式看门狗据此判定僵尸流。
            _markStreamingActivity(msgId);
            _cancelDeliveryTimeoutGracePeriodForSession(sid);
            if (chunkSeq > 0) {
              _observeStreamChunkGap(msgId: msgId, chunkSeq: chunkSeq);
            }
            final placeholder = _buildStreamingPlaceholderFromPayload(
              payload,
              msgId: msgId,
              sessionId: sid,
            );
            if (isCurrentSession && !_currentMessageIds.contains(msgId)) {
              _upsertUIMessageInOrder(placeholder);
            } else {
              _cacheStreamingPlaceholder(placeholder);
            }
            MessageStreamController.addChunk(
              msgId,
              chunk,
              chunkSeq: chunkSeq > 0 ? chunkSeq : null,
            );
            _stageStreamingSessionPreview(
              msgId: msgId,
              sessionId: sid,
              activityAt: placeholder.createdAt,
              isThinking: placeholder.isThinking,
            );
            if (isCurrentSession) {
              final currentOutputState = agentOutputStates[sid];
              final currentOutputStreamMsgId =
                  currentOutputState?['stream_msg_id']?.toString().trim() ?? '';
              if (chunkSeq <= 2 ||
                  currentOutputState == null ||
                  currentOutputStreamMsgId.isEmpty ||
                  currentOutputStreamMsgId != msgId) {
                debugPrint(
                  '📦 stream_chunk visible session=$sid msg_id=$msgId '
                  'chunk_seq=$chunkSeq state=${_describeAgentOutputStateForTrace(currentOutputState)} '
                  'ui=${_describeStreamingUiForTrace(sid, focusMsgId: msgId)}',
                );
              }
            }
          }
          break;

        case 'session_activity_sync':
          if (payload is Map) {
            _applySessionActivitySync(
              SessionActivityModel.fromJson(Map<String, dynamic>.from(payload)),
            );
          }
          break;

        case 'session_activity_list_resp':
          if (payload is Map) {
            final sid = payload['session_id']?.toString().trim() ?? '';
            final rawActivities = List<dynamic>.from(
              payload['activities'] ?? [],
            );
            final activities = rawActivities
                .whereType<Map>()
                .map(
                  (item) => SessionActivityModel.fromJson(
                    Map<String, dynamic>.from(item),
                  ),
                )
                .toList(growable: false);
            _applySessionActivitySnapshot(sid, activities);
          }
          break;

        case 'stream_finish':
          final msgId = payload['msg_id']?.toString();
          final finalContent = payload['final_content'];
          if (msgId != null && finalContent != null) {
            final wasLocallyStopped = _locallyStoppedStreamMsgIds.remove(msgId);
            final rawFinalContent = finalContent.toString();
            final resolvedFinalContent = _resolveStreamingFinalContent(
              msgId: msgId,
              incomingContent: rawFinalContent,
            );
            final senderType = _toInt(payload['sender_type']);
            final normalizedSenderType = senderType > 0 ? senderType : 2;
            final incomingStreamSessionId =
                payload['session_id']?.toString().trim() ?? '';
            final streamSessionId = _resolveStreamingFinalizeSessionId(
              msgId: msgId,
              incomingSessionId: incomingStreamSessionId,
            );
            final finalizedCreatedAt = _resolveStreamFinalizeCreatedAt(
              msgId: msgId,
              incomingCreatedAt: payload['created_at'],
            );
            if (!_isTrackedStreamingMessage(msgId) && !wasLocallyStopped) {
              _discardStreamingSessionPreview(msgId);
              MessageStreamController.discard(msgId);
              // Missed stream_chunk (backgrounded / reconnect) still needs the
              // finalized text as the session preview, otherwise a prior
              // stream_error summary stays stuck on the conversation list.
              if (streamSessionId.isNotEmpty &&
                  resolvedFinalContent.trim().isNotEmpty &&
                  !_shouldSuppressIncomingMessage(
                    content: resolvedFinalContent,
                    senderType: normalizedSenderType,
                  ) &&
                  !ChatMessagePreview.isStandaloneCardMessage(
                    resolvedFinalContent,
                  )) {
                final dict = <String, dynamic>{
                  'msg_id': msgId,
                  'session_id': streamSessionId,
                  'content': resolvedFinalContent,
                  'msg_type': 1,
                  'status': 'success',
                  'sender_id': payload['sender_id']?.toString() ?? '',
                  'sender_type': normalizedSenderType,
                  'created_at': finalizedCreatedAt,
                };
                await _guardDbOp(
                  LocalDb.upsertMessage(dict),
                  op: 'upsertMessage(stream_finish_untracked)',
                );
                await _guardDbOp(
                  _touchSessionByMessage(
                    MessageModel.fromJson(dict),
                    increaseUnread:
                        !_isCurrentSession(streamSessionId) &&
                        !_isMessageFromCurrentUser(
                          dict['sender_id']?.toString() ?? '',
                        ),
                  ),
                  op: 'touchSession(stream_finish_untracked)',
                );
              }
              break;
            }
            if (_shouldSuppressIncomingMessage(
              content: resolvedFinalContent,
              senderType: normalizedSenderType,
            )) {
              _suppressIncomingMessage(
                msgId: msgId,
                sessionId: streamSessionId,
                reason: 'stream_finish_suppressed_tool_gate',
                queueLagMs: _resolveQueueLagMs(enqueuedAtMs),
              );
              _hiddenAgentOutputMessages.remove(msgId);
              _pendingLocalStopStateBySession.remove(streamSessionId);
              _pendingLocalStopStreamMsgIdBySession.remove(streamSessionId);
              break;
            }
            final normalizedFinalContent = resolvedFinalContent;
            _streamDiagFinalize(
              msgId,
              reason: 'stream_finish',
              finalContent: normalizedFinalContent,
              queueLagMs: _resolveQueueLagMs(enqueuedAtMs),
            );
            _activeStreamingMsgIds.remove(msgId);
            _clearStreamChunkGapTrackingForMessage(msgId);
            _streamingPlaceholders.remove(msgId);
            MessageStreamController.finish(msgId, normalizedFinalContent);
            _markSessionComposingResolvedForParticipant(
              streamSessionId,
              participantId: payload['sender_id']?.toString().trim() ?? '',
              participantType: _sessionActivityActorTypeFromSenderType(
                normalizedSenderType,
              ),
              resolvedAt: finalizedCreatedAt,
            );
            _clearSessionComposingActivitiesForMessage(
              streamSessionId,
              msgId: msgId,
              senderId: payload['sender_id']?.toString().trim() ?? '',
              senderType: normalizedSenderType,
            );

            final dict = Map<String, dynamic>.from(payload);
            dict['msg_id'] = msgId;
            if (streamSessionId.isNotEmpty) {
              dict['session_id'] = streamSessionId;
            } else {
              dict.remove('session_id');
            }
            dict['content'] = normalizedFinalContent;
            dict['msg_type'] = 1;
            dict['status'] = 'success';
            dict['created_at'] = finalizedCreatedAt;

            await _guardDbOp(
              LocalDb.upsertMessage(dict),
              op: 'upsertMessage(stream_finish)',
            );
            final msgModel = MessageModel.fromJson(dict);

            if (wasLocallyStopped) {
              // Locally-stopped messages are stashed for possible restoration
              // via agent_output_stop_ack rejection. Do NOT emit a bus event —
              // doing so would re-insert the hidden message into the window.
              _hiddenAgentOutputMessages[msgId] = msgModel;
            } else if (msgId.isNotEmpty) {
              final emitSid = streamSessionId.isNotEmpty
                  ? streamSessionId
                  : dict['session_id']?.toString().trim() ?? '';
              if (emitSid.isNotEmpty) {
                // Use LocalMessagesInserted (not Updated) because stream_finish
                // may finalize a message whose streaming placeholder was not
                // in the window (e.g. non-current session restored on enter).
                // _upsertUIMessageInOrder handles both insert and update via
                // dedup.
                LocalDbChangeBus.instance.emitMessageChange(
                  LocalMessagesInserted(
                    sessionId: emitSid,
                    msgIds: [msgId],
                    maxCreatedAt: _toInt(dict['created_at']),
                    rows: [dict],
                  ),
                );
              }
            }

            final sid = msgModel.sessionId;
            if (sid.isNotEmpty) {
              await _guardDbOp(
                _touchSessionByMessage(
                  msgModel,
                  increaseUnread:
                      !_isCurrentSession(sid) &&
                      !_isMessageFromCurrentUser(msgModel.senderId),
                ),
                op: 'touchSession(stream_finish)',
              );
              _discardStreamingSessionPreview(msgId);
              _markCurrentSessionSeenFromSender(
                sid,
                msgModel.senderId,
                msgId: msgModel.msgId,
              );
              if (wasLocallyStopped) {
                _pendingLocalStopStateBySession.remove(sid);
                _pendingLocalStopStreamMsgIdBySession[sid] = msgId;
              }
              // 非当前会话的审批卡片 → 全局横幅提醒（stream_finish 路径）
              if (!wasLocallyStopped &&
                  !_isCurrentSession(sid) &&
                  !_isMessageFromCurrentUser(msgModel.senderId) &&
                  msgModel.content.contains('grix://card/exec_approval')) {
                final decodedStreamApproval =
                    ChatMessageCardCodec.decodeFromMessage(
                      content: msgModel.content,
                    );
                if (decodedStreamApproval is ChatExecApprovalCardData) {
                  _notifyInAppCardBanner(
                    sessionId: sid,
                    sessionType: _sessionTypeHints[sid.trim()] ?? 'private',
                    approvalCard: decodedStreamApproval,
                  );
                }
              }
            }
            _discardStreamingSessionPreview(msgId);
          }
          break;

        case 'send_ack':
          final clientMsgId = payload['client_msg_id']?.toString();
          final msgId = payload['msg_id']?.toString();
          if (clientMsgId != null && msgId != null) {
            _resetSendAckTimeoutStreak();
            final localStub = await _guardDbOp<Map<String, dynamic>?>(
              LocalDb.getMessageByLocalSeq(clientMsgId),
              op: 'getMessageByLocalSeq(send_ack)',
            );
            final inboxSeq = _toInt(payload['inbox_seq']);
            final createdAt = _normalizeMessageCreatedAt(
              _toInt(payload['created_at']),
            );
            _observeInboxSeq(inboxSeq);
            _cancelSendAckTimer(clientMsgId);
            await _guardDbOp(
              LocalDb.updateAckMsg(
                clientMsgId,
                msgId,
                inboxSeq,
                createdAt: createdAt,
              ),
              op: 'updateAckMsg(send_ack)',
            );
            // Emit bus event for the subscription path. Also call _ackUIMessage
            // directly as a reliable fallback — the bus subscription may not be
            // active when _currentSessionId is not set (e.g. test environment,
            // session transition race). _ackUIMessage → _upsertUIMessageInOrder
            // is idempotent, so double-processing from both paths is harmless.
            if (localStub != null) {
              final ackSid = localStub['session_id']?.toString().trim() ?? '';
              if (ackSid.isNotEmpty && msgId.isNotEmpty) {
                LocalDbChangeBus.instance.emitMessageChange(
                  LocalMessageUpdated(
                    sessionId: ackSid,
                    msgId: msgId,
                    clientMsgId: clientMsgId,
                    ackCreatedAt: createdAt,
                  ),
                );
              }
            } else if (_currentSessionId.value != null && msgId.isNotEmpty) {
              LocalDbChangeBus.instance.emitMessageChange(
                LocalMessageUpdated(
                  sessionId: _currentSessionId.value!,
                  msgId: msgId,
                  clientMsgId: clientMsgId,
                  ackCreatedAt: createdAt,
                ),
              );
            }
            _ackUIMessage(clientMsgId, msgId, createdAt);
            if (createdAt > 0 && localStub != null) {
              final sid = localStub['session_id']?.toString().trim() ?? '';
              if (sid.isNotEmpty) {
                final content = localStub['content']?.toString() ?? '';
                await _guardDbOp(
                  _touchSession(
                    sid,
                    content,
                    createdAt,
                    type: resolveSessionTypeById(sid),
                    increaseUnread: false,
                  ),
                  op: 'touchSession(send_ack)',
                );
              }
            }

            final localInference = payload['local_inference'];
            if (localInference is Map<String, dynamic>) {
              unawaited(_handleLocalInference(localInference, msgId));
            }
          }
          break;

        case 'relay_local_stream_start_ack':
          _handleRelayLocalStreamStartAck(payload);
          break;

        case 'send_nack':
          final clientMsgId = payload['client_msg_id']?.toString();
          if (clientMsgId != null && clientMsgId.isNotEmpty) {
            _resetSendAckTimeoutStreak();
            await _handleSendNack(
              clientMsgId,
              code: _toInt(payload['code']),
              message: payload['msg']?.toString() ?? '',
            );
          }
          break;

        case 'retry_msg_ack':
          final msgId = payload['msg_id']?.toString().trim() ?? '';
          final code = _toInt(payload['code']);
          final message = payload['msg']?.toString().trim() ?? '';
          if (msgId.isEmpty) {
            break;
          }
          if (code == 0) {
            await _guardDbOp(
              LocalDb.updateAgentDeliveryStatusByMsgId(msgId, 'queued'),
              op: 'updateAgentDeliveryStatusByMsgId(retry_msg_ack)',
            );
            final updatedRow = await _guardDbOp<Map<String, dynamic>?>(
              LocalDb.getMessageByMsgId(msgId),
              op: 'getMessageByMsgId(retry_msg_ack)',
            );
            if (_currentSessionId.value != null) {
              LocalDbChangeBus.instance.emitMessageChange(
                LocalMessageUpdated(
                  sessionId: _currentSessionId.value!,
                  msgId: msgId,
                  row: updatedRow,
                ),
              );
            }
            // Direct UI update as fallback when bus subscription is not active
            // or DB row is unavailable (e.g. test env without LocalDb).
            // Idempotent — safe to call alongside the bus event path.
            _updateAgentDeliveryStatusInWindow(msgId, 'queued');
            break;
          }
          CustomToast.show(
            message.isNotEmpty ? message : 'common_error'.tr,
            isError: true,
          );
          break;

        case 'stream_error':
          final msgId = payload['msg_id']?.toString();
          if (msgId != null) {
            final wasLocallyStopped = _locallyStoppedStreamMsgIds.remove(msgId);
            final sid = _resolveStreamingFinalizeSessionId(
              msgId: msgId,
              incomingSessionId: payload['session_id']?.toString().trim() ?? '',
            );
            final finalizedCreatedAt = _resolveStreamFinalizeCreatedAt(
              msgId: msgId,
              incomingCreatedAt: payload['created_at'],
            );
            final shouldHandleError =
                wasLocallyStopped ||
                _isTrackedStreamingMessage(msgId) ||
                sid.isNotEmpty ||
                _hasMessageInCurrentWindow(msgId);
            if (!shouldHandleError) {
              MessageStreamController.discard(msgId);
              break;
            }
            final senderType = _toInt(payload['sender_type']);
            final normalizedSenderType = senderType > 0 ? senderType : 2;
            _activeStreamingMsgIds.remove(msgId);
            _clearStreamChunkGapTrackingForMessage(msgId);
            final stream = MessageStreamController.getStream(msgId);
            final rawFallback = stream.value.isNotEmpty
                ? stream.value
                : (payload['error_msg']?.toString() ?? '');
            if (_shouldSuppressIncomingMessage(
              content: rawFallback,
              senderType: normalizedSenderType,
            )) {
              _suppressIncomingMessage(
                msgId: msgId,
                sessionId: sid,
                reason: 'stream_error_suppressed_tool_gate',
                queueLagMs: _resolveQueueLagMs(enqueuedAtMs),
              );
              _hiddenAgentOutputMessages.remove(msgId);
              _pendingLocalStopStateBySession.remove(sid);
              _pendingLocalStopStreamMsgIdBySession.remove(sid);
              break;
            }
            final fallback = rawFallback;
            _streamDiagFinalize(
              msgId,
              reason: 'stream_error',
              finalContent: fallback,
              queueLagMs: _resolveQueueLagMs(enqueuedAtMs),
            );
            _streamingPlaceholders.remove(msgId);
            MessageStreamController.finish(msgId, fallback);
            _clearSessionComposingActivitiesForMessage(
              sid,
              msgId: msgId,
              senderId: payload['sender_id']?.toString().trim() ?? '',
              senderType: normalizedSenderType,
            );

            final dict = <String, dynamic>{
              'msg_id': msgId,
              'sender_id': payload['sender_id']?.toString() ?? '',
              'msg_type': 1,
              'content': fallback,
              'status': 'error',
              'created_at': finalizedCreatedAt,
            };
            if (sid.isNotEmpty) {
              dict['session_id'] = sid;
            }

            await _guardDbOp(
              LocalDb.upsertMessage(dict),
              op: 'upsertMessage(stream_error)',
            );
            final msgModel = MessageModel.fromJson(dict);
            if (wasLocallyStopped) {
              // Locally-stopped messages are stashed for possible restoration
              // via agent_output_stop_ack rejection. Do NOT emit a bus event.
              _hiddenAgentOutputMessages[msgId] = msgModel;
            } else if (msgId.isNotEmpty && sid.isNotEmpty) {
              // Use LocalMessagesInserted (not Updated) because stream_error
              // may create a brand-new message (e.g. delegate error with no
              // prior stream_chunk). _upsertUIMessageInOrder handles both
              // insert and update via dedup.
              LocalDbChangeBus.instance.emitMessageChange(
                LocalMessagesInserted(
                  sessionId: sid,
                  msgIds: [msgId],
                  maxCreatedAt: _toInt(dict['created_at']),
                  rows: [dict],
                ),
              );
            }
            if (sid.isNotEmpty) {
              await _guardDbOp(
                _touchSessionByMessage(
                  msgModel,
                  increaseUnread:
                      !_isCurrentSession(sid) &&
                      !_isMessageFromCurrentUser(msgModel.senderId),
                ),
                op: 'touchSession(stream_error)',
              );
              _discardStreamingSessionPreview(msgId);
              _markCurrentSessionSeenFromSender(
                sid,
                msgModel.senderId,
                msgId: msgModel.msgId,
              );
              if (wasLocallyStopped) {
                _pendingLocalStopStateBySession.remove(sid);
                _pendingLocalStopStreamMsgIdBySession[sid] = msgId;
              }
            }
            _discardStreamingSessionPreview(msgId);
          }
          break;

        case 'delegate_ack':
          final sessionId = payload['session_id']?.toString() ?? '';
          final agentId = _toId(payload['agent_id']);
          final active = payload['active'] == true;
          final maxConsecutive = _resolveDelegateMaxConsecutiveReplies(
            payload['max_consecutive_replies'],
            sessionId: sessionId,
          );
          if (active) {
            delegateStates[sessionId] = {
              'agent_id': agentId,
              'active': true,
              'max_consecutive_replies': maxConsecutive,
              'channel_unavailable': false,
            };
          } else {
            delegateStates.remove(sessionId);
          }
          break;

        case 'delegate_list_resp':
          final delegates = List<Map<String, dynamic>>.from(
            payload['delegates'] ?? [],
          );
          delegateStates.clear();
          for (var d in delegates) {
            final sessionId = d['session_id']?.toString() ?? '';
            final agentId = _toId(d['agent_id']);
            final maxConsecutive = _resolveDelegateMaxConsecutiveReplies(
              d['max_consecutive_replies'],
              sessionId: sessionId,
            );
            if (sessionId.isNotEmpty && agentId.isNotEmpty) {
              delegateStates[sessionId] = {
                'agent_id': agentId,
                'active': true,
                'max_consecutive_replies': maxConsecutive,
                'channel_unavailable': false,
              };
            }
          }
          break;

        case 'agent_delivery_error':
          if (payload is Map) {
            _handleAgentDeliveryError(Map<String, dynamic>.from(payload));
          }
          break;

        case 'agent_delivery_status':
          if (payload is Map) {
            await _handleAgentDeliveryStatus(
              Map<String, dynamic>.from(payload),
            );
          }
          break;

        case 'agent_delivery_status_batch':
          if (payload is Map) {
            await _handleAgentDeliveryStatusBatch(
              Map<String, dynamic>.from(payload),
            );
          }
          break;

        case 'agent_output_get_resp':
          if (payload is Map) {
            _applyAgentOutputSnapshot(Map<String, dynamic>.from(payload));
          }
          break;

        case 'agent_toolbar_get_resp':
          if (payload is Map) {
            _applyAgentToolbarSnapshotPayload(
              Map<String, dynamic>.from(payload),
            );
          }
          break;

        case 'agent_toolbar_sync':
          if (payload is Map) {
            _applyAgentToolbarSnapshotPayload(
              Map<String, dynamic>.from(payload),
            );
          }
          break;

        case 'agent_toolbar_action_ack':
          if (payload is Map) {
            _handleAgentToolbarActionAck(Map<String, dynamic>.from(payload));
          }
          break;

        case 'conversation_audit_set_resp':
          if (payload is Map) {
            _handleConversationAuditSetResp(Map<String, dynamic>.from(payload));
          }
          break;

        case 'event_state':
          if (payload is Map) {
            _handleEventState(Map<String, dynamic>.from(payload));
          }
          break;

        case 'event_cancel_result':
          if (payload is Map) {
            _handleEventCancelResult(Map<String, dynamic>.from(payload));
          }
          break;

        case 'queue_clear_result':
          if (payload is Map) {
            _handleQueueClearResult(Map<String, dynamic>.from(payload));
          }
          break;

        case 'queue_reorder_result':
          if (payload is Map) {
            _handleQueueReorderResult(Map<String, dynamic>.from(payload));
          }
          break;

        case 'event_hold_result':
          if (payload is Map) {
            _handleEventHoldResult(Map<String, dynamic>.from(payload));
          }
          break;

        case 'queue_edit_result':
          if (payload is Map) {
            _handleQueueEditResult(Map<String, dynamic>.from(payload));
          }
          break;

        case 'queue_snapshot':
          if (payload is Map) {
            _handleQueueSnapshot(Map<String, dynamic>.from(payload));
          }
          break;

        case 'agent_session_bind_resp':
          final bindSeq = data['seq'];
          if (bindSeq is int && _sessionBindPending.containsKey(bindSeq)) {
            final completer = _sessionBindPending.remove(bindSeq)!;
            if (!completer.isCompleted) {
              final err = payload is Map ? payload['error']?.toString() : null;
              if (err != null && err.isNotEmpty) {
                final code = payload is Map
                    ? payload['code']?.toString().trim() ?? ''
                    : '';
                completer.completeError(
                  Exception(code.isNotEmpty ? '$code: $err' : err),
                );
              } else if (payload is Map) {
                completer.complete(Map<String, dynamic>.from(payload));
              } else {
                completer.completeError(
                  Exception('im_session_bind_invalid_response'.tr),
                );
              }
            }
          }
          break;

        case 'agent_output_stop_ack':
          if (payload is Map) {
            _handleAgentOutputStopAck(Map<String, dynamic>.from(payload));
          }
          break;

        case 'mcp_frame':
          if (payload is Map) {
            await _handleMcpFrame(Map<String, dynamic>.from(payload));
          }
          break;

        case 'agent_output_status':
          if (payload is Map) {
            _handleAgentOutputStatus(Map<String, dynamic>.from(payload));
          }
          break;

        case 'pull_sync_resp':
          await _ensureDeletedSessionsLoaded();
          await _ensureInboxSeqCursorLoaded();
          final batchCursorBefore = _lastInboxSeqCursor;
          final hasMore = payload['has_more'] == true;
          final msgs = List<Map<String, dynamic>>.from(
            payload['messages'] ?? [],
          );
          int batchMaxInboxSeq = 0;
          // 本批新消息是否确实落盘成功。仅在成功时才允许推进 inbox_seq 游标并
          // 继续 has_more 拉取；失败则保留旧游标并安排重拉，避免跳过未落盘的消息。
          bool acceptedPersistOk = true;
          bool editedPersistOk = true;
          bool revokedPersistOk = true;
          final revokedMsgs = <Map<String, dynamic>>[];
          final revokedSessionIDs = <String>{};
          final editedMsgs = <Map<String, dynamic>>[];
          final editedSessionIDs = <String>{};
          final hasUnreadSnapshot =
              payload is Map && payload.containsKey('unread_snapshot');
          // 服务端在每一批 pull_sync_resp 都会带上 unread_snapshot（基于
          // session_members.unread_count 的当前权威快照）。多轮拉取（hasMore=true）
          // 时若每批都整体替换未读数，会因本地消息尚未拉全而出现未读数抖动。
          // 仅在最后一批（hasMore=false，即本地已追平服务端差量）才应用快照，
          // 等价于服务端 snapshot_seq 想表达的"已追平才信任快照"语义；hasMore 在
          // 客户端侧比 snapshot_seq 更可靠（后者与消息查询存在间隙竞态，可能恒大于
          // 本批 batchMaxInboxSeq 而导致快照永不应用）。
          final shouldApplyUnreadSnapshot = hasUnreadSnapshot && !hasMore;
          final unreadSnapshot = _normalizeUnreadSnapshot(
            _parseUnreadSnapshot(payload['unread_snapshot']),
          );
          // active_voice_calls 与 unread_snapshot 同语义：服务端权威全量快照，
          // 仅在已追平（hasMore=false）时整份覆盖本地"语音中"徽标。
          // 老后端不带该字段（null）时跳过，不误清本地状态。
          final hasVoiceSnapshot =
              payload is Map &&
              payload.containsKey('active_voice_calls') &&
              payload['active_voice_calls'] != null;
          final shouldApplyVoiceSnapshot = hasVoiceSnapshot && !hasMore;
          if (msgs.isNotEmpty) {
            final acceptedMsgs = <Map<String, dynamic>>[];
            for (final raw in msgs) {
              final row = Map<String, dynamic>.from(raw);
              final inboxSeq = _toInt(row['inbox_seq']);
              if (inboxSeq > batchMaxInboxSeq) {
                batchMaxInboxSeq = inboxSeq;
              }
              final sid = row['session_id']?.toString().trim() ?? '';
              if (sid.isEmpty) {
                continue;
              }
              var createdAt = _toInt(row['created_at']);
              if (createdAt > 0 && createdAt < 10000000000) {
                createdAt = createdAt * 1000;
              }
              if (!_shouldAcceptDeletedSessionActivity(sid, createdAt)) {
                continue;
              }
              row['created_at'] = createdAt;
              final incomingMsgId = row['msg_id']?.toString() ?? '';
              final isRevoked = row['is_revoked'] == true;
              if (isRevoked) {
                if (incomingMsgId.isNotEmpty) {
                  final persisted = await _applyLocalMessageRevokeImpl(
                    sessionId: sid,
                    msgId: incomingMsgId,
                    dbOpLabel: 'deleteMessage(pull_sync_resp_revoke)',
                    reloadSessions: false,
                  );
                  if (persisted) {
                    revokedSessionIDs.add(sid);
                  } else {
                    revokedPersistOk = false;
                  }
                }
                continue;
              }
              final incomingMsgType = _toInt(row['msg_type']);
              if (incomingMsgId.isNotEmpty && incomingMsgType != 4) {
                final incomingContent = row['content']?.toString() ?? '';
                final resolvedContent = _resolveStreamingFinalContent(
                  msgId: incomingMsgId,
                  incomingContent: incomingContent,
                );
                if (resolvedContent != incomingContent) {
                  row['content'] = resolvedContent;
                }
              }
              if (_toBool(row['is_revoked'])) {
                revokedMsgs.add(row);
                continue;
              }
              final syncEvent = row['sync_event']?.toString().trim() ?? '';
              if (syncEvent == 'edit') {
                editedMsgs.add(row);
                continue;
              }
              if (_shouldSuppressIncomingMessageMap(row)) {
                final msgId = row['msg_id']?.toString() ?? '';
                _suppressIncomingMessage(
                  msgId: msgId,
                  sessionId: sid,
                  reason: 'pull_sync_suppressed_tool_gate',
                  queueLagMs: _resolveQueueLagMs(enqueuedAtMs),
                );
                continue;
              }
              acceptedMsgs.add(row);
            }

            final localMaxBefore =
                await _guardDbOp<int>(
                  LocalDb.getMaxInboxSeq(),
                  op: 'getMaxInboxSeq(pull_sync_resp)',
                  fallback: 0,
                ) ??
                0;
            if (acceptedMsgs.isNotEmpty) {
              acceptedPersistOk = await _guardDbWrite(
                () => LocalDb.batchInsertMessages(acceptedMsgs),
                op: 'batchInsertMessages(pull_sync_resp)',
              );
              // Emit per-session inserted events for the change bus.
              final insertedBySession = <String, List<Map<String, dynamic>>>{};
              for (final row in acceptedMsgs) {
                final rowSid = row['session_id']?.toString().trim() ?? '';
                if (rowSid.isNotEmpty) {
                  insertedBySession.putIfAbsent(rowSid, () => []).add(row);
                }
              }
              for (final entry in insertedBySession.entries) {
                final ids = entry.value
                    .map((r) => r['msg_id']?.toString().trim() ?? '')
                    .where((id) => id.isNotEmpty)
                    .toList();
                if (ids.isNotEmpty) {
                  final maxTs = entry.value
                      .map((r) => _toInt(r['created_at']))
                      .fold<int>(0, (a, b) => a > b ? a : b);
                  LocalDbChangeBus.instance.emitMessageChange(
                    LocalMessagesInserted(
                      sessionId: entry.key,
                      msgIds: ids,
                      maxCreatedAt: maxTs,
                      rows: entry.value,
                    ),
                  );
                }
              }
            }
            if (editedMsgs.isNotEmpty) {
              editedPersistOk = await _guardDbWrite(
                () => LocalDb.batchUpsertMessages(editedMsgs),
                op: 'batchUpsertMessages(pull_sync_resp_edit)',
              );
            }
            if (editedPersistOk) {
              for (final row in editedMsgs) {
                final sid = row['session_id']?.toString().trim() ?? '';
                final mid = row['msg_id']?.toString().trim() ?? '';
                if (sid.isNotEmpty) {
                  editedSessionIDs.add(sid);
                }
                if (sid.isNotEmpty && mid.isNotEmpty) {
                  LocalDbChangeBus.instance.emitMessageChange(
                    LocalMessageUpdated(sessionId: sid, msgId: mid, row: row),
                  );
                }
                if (sid.isEmpty || mid.isEmpty) {
                  continue;
                }
              }
            }

            final authService = Get.find<AuthService>();
            final myUserId = authService.userId ?? '';
            final sessionDelta = <String, Map<String, dynamic>>{};
            final currentSessionIncoming = <MessageModel>[];

            for (final row in acceptedMsgs) {
              final inboxSeq = _toInt(row['inbox_seq']);
              if (inboxSeq <= localMaxBefore) {
                continue;
              }

              final sid = row['session_id']?.toString().trim() ?? '';
              if (sid.isEmpty) {
                continue;
              }

              final createdAt = _toInt(row['created_at']);
              final content = row['content']?.toString() ?? '';
              final senderId = _toId(row['sender_id']);
              final senderType = _toInt(row['sender_type']);
              final sessionType = _normalizeSessionTypeFromWire(
                row['session_type'],
                fallback: _sessionTypeHints[sid] ?? 'private',
              );
              _sessionTypeHints[sid] = sessionType;
              unawaited(
                _hydratePrivateSessionTitle(
                  sessionId: sid,
                  sessionType: sessionType,
                  senderId: senderId,
                  senderType: senderType,
                ),
              );
              final isMine =
                  myUserId.isNotEmpty &&
                  senderType == 1 &&
                  senderId == myUserId;
              final shouldIncreaseUnread =
                  !hasUnreadSnapshot && !isMine && !_isCurrentSession(sid);

              if (_isCurrentSession(sid)) {
                final incoming = MessageModel.fromJson(row);
                if (incoming.msgType != 4 && incoming.msgId.isNotEmpty) {
                  final wasStreaming = _activeStreamingMsgIds.remove(
                    incoming.msgId,
                  );
                  if (wasStreaming) {
                    _discardStreamingSessionPreview(incoming.msgId);
                    _clearStreamChunkGapTrackingForMessage(incoming.msgId);
                    _streamDiagFinalize(
                      incoming.msgId,
                      reason: 'pull_sync_non_stream',
                      finalContent: incoming.content,
                      queueLagMs: _resolveQueueLagMs(enqueuedAtMs),
                    );
                    _streamingPlaceholders.remove(incoming.msgId);
                    MessageStreamController.finish(
                      incoming.msgId,
                      incoming.content,
                    );
                  }
                }
                currentSessionIncoming.add(incoming);
                _markCurrentSessionSeenFromSender(
                  sid,
                  senderId,
                  msgId: incoming.msgId,
                );
              }

              // Tool messages (msg_type=4) and standalone card messages
              // (grix://card links) advance the session timestamp but must not
              // overwrite the preview text with non-human-readable content.
              final rowMsgType = _toInt(row['msg_type']);
              final contentForPreview =
                  rowMsgType == 4 ||
                      ChatMessagePreview.isStandaloneCardMessage(content)
                  ? ''
                  : content;

              final delta = sessionDelta.putIfAbsent(sid, () {
                return {
                  'last_content': contentForPreview,
                  'last_created_at': createdAt,
                  'unread_inc': 0,
                  'peer_id': '',
                  'peer_type': 0,
                };
              });

              final prevTs = _toInt(delta['last_created_at']);
              if (createdAt >= prevTs) {
                delta['last_created_at'] = createdAt;
                if (contentForPreview.isNotEmpty) {
                  delta['last_content'] = contentForPreview;
                }
              }
              if (shouldIncreaseUnread) {
                delta['unread_inc'] = _toInt(delta['unread_inc']) + 1;
              }
              // 随 delta 落库对端身份，让新建会话行从插入起就带 peer_id/
              // peer_type，避免产生无 peer 占位行导致会话列表归组键失配、
              // 未读角标与底部栏对不上（需等 peer 身份补拉才恢复）。
              //
              // 优先用载荷里的 session_members（任何 sender_type 都适用），
              // 旧服务端没有该字段时才退回「发送者非本人即对端」的老口径。
              if (sessionType == 'private' &&
                  _toInt(delta['peer_type']) == 0) {
                final rowPeer = _peerIdentityFromMessageMembers(
                  row['session_members'],
                );
                if (rowPeer.peerId.isNotEmpty) {
                  delta['peer_id'] = rowPeer.peerId;
                  delta['peer_type'] = rowPeer.peerType;
                } else if (!isMine &&
                    (senderType == 1 || senderType == 2) &&
                    senderId.isNotEmpty) {
                  delta['peer_id'] = senderId;
                  delta['peer_type'] = senderType;
                }
              }
            }

            // Batch-apply session deltas in a single DB transaction instead of
            // N individual updateSessionLastMsg + incrementUnreadBy calls.
            if (!hasUnreadSnapshot && sessionDelta.isNotEmpty) {
              await _guardDbOp(
                LocalDb.batchApplySessionDeltas(
                  sessionDelta.map(
                    (k, v) => MapEntry(k, {
                      'last_content': v['last_content']?.toString() ?? '',
                      'last_created_at': _toInt(v['last_created_at']),
                      'unread_inc': _toInt(v['unread_inc']),
                      'peer_id': v['peer_id']?.toString() ?? '',
                      'peer_type': _toInt(v['peer_type']),
                    }),
                  ),
                  _sessionTypeHints,
                ),
                op: 'batchApplySessionDeltas(pull_sync_resp)',
              );
            } else if (sessionDelta.isNotEmpty) {
              // Unread snapshot mode: only update last message, skip unread inc.
              await _guardDbOp(
                LocalDb.batchApplySessionDeltas(
                  sessionDelta.map(
                    (k, v) => MapEntry(k, {
                      'last_content': v['last_content']?.toString() ?? '',
                      'last_created_at': _toInt(v['last_created_at']),
                      'unread_inc': 0,
                      'peer_id': v['peer_id']?.toString() ?? '',
                      'peer_type': _toInt(v['peer_type']),
                    }),
                  ),
                  _sessionTypeHints,
                ),
                op: 'batchApplySessionDeltas(pull_sync_resp,snapshot)',
              );
            }

            if (currentSessionIncoming.isNotEmpty &&
                !ImService._dbChangeEventDrivenWindow) {
              currentSessionIncoming.sort((a, b) => _compareMessageOrder(a, b));
              for (final msg in currentSessionIncoming) {
                _appendUIMessage(msg);
              }
            }

            for (final row in revokedMsgs) {
              final sid = row['session_id']?.toString().trim() ?? '';
              final persisted = await _consumeIncomingRevokedMessage(
                row,
                dbOpLabel: 'deleteMessage(pull_sync_revoke)',
                reloadSessions: false,
              );
              if (persisted && sid.isNotEmpty) {
                revokedSessionIDs.add(sid);
              } else if (!persisted) {
                revokedPersistOk = false;
              }
            }
            // 撤回：本地已无可预览消息时清空摘要，避免被撤回的内容留在会话列表。
            for (final sid in revokedSessionIDs) {
              await _refreshSessionPreviewFromLocal(
                sid,
                allowClearPreview: true,
              );
            }
            for (final sid in editedSessionIDs) {
              if (revokedSessionIDs.contains(sid)) continue;
              await _refreshSessionPreviewFromLocal(sid);
            }
          }
          // 仅在本批新消息/编辑/撤回确实落盘成功后才推进游标。若落盘失败
          // 则保留旧游标，下方安排重拉，避免跳过未落盘的新消息、编辑或撤回。
          if (acceptedPersistOk && editedPersistOk && revokedPersistOk) {
            _observeInboxSeq(batchMaxInboxSeq);
          }
          if (shouldApplyUnreadSnapshot) {
            // Apply local unread overrides so the DB write does not overwrite
            // a recent clearUnread/markUnread that the server hasn't ACK'd.
            if (_localUnreadOverrides.isNotEmpty) {
              for (final entry in _localUnreadOverrides.entries) {
                final sid = entry.key;
                final overrideUnread = entry.value.unreadCount;
                if (overrideUnread <= 0) {
                  unreadSnapshot.remove(sid);
                } else {
                  unreadSnapshot[sid] = overrideUnread;
                }
              }
            }
            await _guardDbOp(
              LocalDb.replaceUnreadCounts(unreadSnapshot),
              op: 'replaceUnreadCounts(pull_sync_resp)',
            );
            final currentSessionId = _currentSessionId.value?.trim() ?? '';
            if (currentSessionId.isNotEmpty) {
              await _guardDbOp(
                LocalDb.clearUnread(currentSessionId),
                op: 'clearPendingReadSessionUnread(pull_sync_resp)',
              );
            }
          }
          if (shouldApplyVoiceSnapshot) {
            // 以服务端为准整份覆盖"语音中"徽标，自愈离线期间漏收的开始/结束通知。
            final rawVoiceCalls = (payload['active_voice_calls'] as List)
                .whereType<Map>()
                .map((e) => Map<String, dynamic>.from(e))
                .toList();
            applyVoiceCallSnapshot(rawVoiceCalls);
          }
          // loadSessions reads all sessions from DB in one pass, so no need
          // to call _refreshSessionPreviewFromLocal per session individually.
          //
          // 多批补拉（hasMore 链）期间按最小间隔节流整刷：大量积压时每批 100 条
          // 都整刷一次会把补拉链拖成 O(批数×全量刷新)，冷启动会被拖垮。节流只
          // 作用于"链还会继续"的中间批；链在本批停止（末批，或 hasMore=true 但
          // 本批消息被服务端整批过滤为空、下方不会续拉）都必刷收尾，保证已落盘
          // 的前批消息不会因节流滞留在列表之外。未读快照本就只在末批应用，
          // 节流不影响其语义。
          final drainChainContinues =
              acceptedPersistOk &&
              revokedPersistOk &&
              hasMore &&
              msgs.isNotEmpty;
          final nowMs = DateTime.now().millisecondsSinceEpoch;
          final drainRefreshDue =
              nowMs - _lastPullSyncDrainSessionsRefreshMs >=
              ImService._pullSyncDrainSessionsRefreshInterval.inMilliseconds;
          if (!drainChainContinues || drainRefreshDue) {
            _lastPullSyncDrainSessionsRefreshMs = nowMs;
            await loadSessions(refreshFromServer: false);
            await _syncDeferredSystemUnreadBadgeAfterAuthoritativeRefresh();
          }
          if (!acceptedPersistOk || !editedPersistOk || !revokedPersistOk) {
            // 本批消息/编辑落盘失败：游标未推进，用旧游标退避重拉，把本批
            // 重新拉回落盘，避免 pull_sync 主链丢消息或丢编辑。
            _triggerPullSyncAfterPersistFailure(
              cursorOverride: batchCursorBefore,
            );
          } else if (hasMore && msgs.isNotEmpty) {
            _triggerPullSync();
          }
          break;

        case 'session_read_ack':
          final sid = payload['session_id']?.toString().trim() ?? '';
          final code = _toInt(payload['code']);
          final lastReadMsgId =
              payload['last_read_msg_id']?.toString().trim() ?? '';
          if (sid.isNotEmpty) {
            _handleSessionReadAck(
              sid,
              code: code,
              lastReadMsgId: lastReadMsgId,
            );
          }
          break;

        case 'kicked':
          _cancelAuthHandshakeTimer();
          final reason = payload['reason']?.toString().trim() ?? 'unknown';
          debugPrint('⚠️ Kicked by server: $reason');
          _allowReconnect = false;
          disconnect(stage: ImConnectionStage.kicked);
          if (_shouldPreserveLoginOnKick(reason)) {
            debugPrint('ℹ️ Preserve local auth after kick: $reason');
            break;
          }
          await Get.find<AuthService>().logout();
          _redirectToLogin();
          break;

        case 're_auth_ack':
          _cancelAuthHandshakeTimer();
          final reAuthCode = payload['code'];
          if (reAuthCode != 0) {
            debugPrint('❌ re_auth failed: code=$reAuthCode ${payload['msg']}');
            if (reAuthCode == ImService.authCodeRetryable) {
              // 服务端临时故障，凭证没问题：别去刷凭证，保留会话重连即可。
              debugPrint('⚠️ Server temporarily unavailable, keep session');
              _allowReconnect = true;
              _handleDisconnect(finalStage: ImConnectionStage.reconnecting);
            } else {
              await _handleWsCredentialFailure(reAuth: true);
            }
          } else {
            _resetSendAckTimeoutStreak();
            final expiresIn = _toInt(payload['expires_in']);
            if (expiresIn > 0) {
              await Get.find<AuthService>().updateAccessExpiryFromServer(
                expiresIn,
              );
            }
            _restoreCurrentSessionRealtimeState();
          }
          break;

        case 'agent_state_sync':
          _handleAgentStateSync(payload);
          break;

        case 'session_history_reset_ack':
          _handleSessionHistoryResetAck(payload, seq: _toInt(data['seq']));
          break;

        case 'session_history_reset_sync':
          _handleSessionHistoryResetSync(payload);
          break;

        case 'session_history_resets_query_ack':
          _handleSessionHistoryResetsQueryAck(payload);
          break;

        case 'push_revoke':
          final revokeInboxSeq = _toInt(payload['inbox_seq']);
          final prevRevokeInboxSeq = _lastInboxSeqCursor;
          int? authoritativeUnreadCount;
          if (revokeInboxSeq > prevRevokeInboxSeq &&
              payload is Map &&
              payload.containsKey('session_unread_count')) {
            authoritativeUnreadCount = _toInt(payload['session_unread_count']);
          }
          if (revokeInboxSeq > 0 &&
              prevRevokeInboxSeq > 0 &&
              revokeInboxSeq > prevRevokeInboxSeq + 1) {
            debugPrint(
              '⚠️ revoke inbox_seq gap detected: prev=$prevRevokeInboxSeq incoming=$revokeInboxSeq',
            );
            _triggerPullSyncThrottled(cursorOverride: prevRevokeInboxSeq);
          }
          if (payload is! Map) {
            break;
          }
          final revokePayload = Map<String, dynamic>.from(payload);
          final revokeSessionId =
              revokePayload['session_id']?.toString().trim() ?? '';
          final revokeMsgId = revokePayload['msg_id']?.toString().trim() ?? '';
          if (revokeSessionId.isEmpty || revokeMsgId.isEmpty) break;
          final revokePersisted = await _applyLocalMessageRevokeImpl(
            sessionId: revokeSessionId,
            msgId: revokeMsgId,
            dbOpLabel: 'deleteMessage(push_revoke)',
            authoritativeUnreadCount: authoritativeUnreadCount,
          );
          if (revokePersisted) {
            _observeInboxSeq(revokeInboxSeq);
          } else {
            if (prevRevokeInboxSeq > 0 && revokeInboxSeq > prevRevokeInboxSeq) {
              _triggerPullSyncAfterPersistFailure(
                cursorOverride: prevRevokeInboxSeq,
              );
            } else {
              _triggerPullSyncAfterPersistFailure();
            }
          }
          break;

        case 'push_edit':
          final editInboxSeq = _toInt(payload['inbox_seq']);
          final prevEditInboxSeq = _lastInboxSeqCursor;
          if (editInboxSeq > 0 &&
              prevEditInboxSeq > 0 &&
              editInboxSeq > prevEditInboxSeq + 1) {
            debugPrint(
              '⚠️ edit inbox_seq gap detected: prev=$prevEditInboxSeq incoming=$editInboxSeq',
            );
            _triggerPullSyncThrottled(cursorOverride: prevEditInboxSeq);
          }
          if (payload is! Map) {
            break;
          }
          final editRow = Map<String, dynamic>.from(payload);
          final editSessionId = editRow['session_id']?.toString().trim() ?? '';
          final editMsgId = editRow['msg_id']?.toString().trim() ?? '';
          if (editSessionId.isEmpty || editMsgId.isEmpty) {
            break;
          }
          var editCreatedAt = _toInt(editRow['created_at']);
          if (editCreatedAt > 0 && editCreatedAt < 10000000000) {
            editCreatedAt = editCreatedAt * 1000;
          }
          editRow['created_at'] = editCreatedAt;
          if (!_shouldAcceptDeletedSessionActivity(
            editSessionId,
            editCreatedAt,
          )) {
            _observeInboxSeq(editInboxSeq);
            break;
          }
          final editPersisted = await _guardDbWrite(
            () => LocalDb.upsertMessage(editRow),
            op: 'upsertMessage(push_edit)',
          );
          if (!editPersisted) {
            if (prevEditInboxSeq > 0 && editInboxSeq > prevEditInboxSeq) {
              _triggerPullSyncAfterPersistFailure(
                cursorOverride: prevEditInboxSeq,
              );
            } else {
              _triggerPullSyncAfterPersistFailure();
            }
            break;
          }
          _observeInboxSeq(editInboxSeq);
          LocalDbChangeBus.instance.emitMessageChange(
            LocalMessageUpdated(
              sessionId: editSessionId,
              msgId: editMsgId,
              row: editRow,
            ),
          );
          await _queueSessionPreviewFromEditedMessage(editRow);
          break;

        // 语音通话信令（Phase 1）
        case 'call:invite_ack':
          _handleCallInviteAck(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:ring':
          _handleCallRing(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:peer_answered':
          _handleCallPeerAnswered(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:ai_delegated':
          _handleCallAiDelegated(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:listen_ack':
          _handleCallListenAck(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:voice_status_end':
          _handleCallVoiceStatusEnd(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:state':
        case 'call:timeout':
        case 'call:busy':
          _handleCallState(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:voice_delegate_ack':
          _handleCallVoiceDelegateAck(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:queued':
          _handleCallQueued(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:queue_update':
          _handleCallQueueUpdate(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
        case 'call:queue_expired':
          _handleCallQueueExpired(
            Map<String, dynamic>.from(payload is Map ? payload : {}),
          );
          break;
      }
    } catch (e, st) {
      debugPrint('❌ Parse error: $e\n$st');
    }
  }

  Future<void> _applyAuthAckInboxBootstrap(dynamic payload) async {
    if (payload is! Map) {
      return;
    }
    final latestInboxSeq = _toInt(payload['latest_inbox_seq']);
    if (latestInboxSeq <= 0) {
      return;
    }

    await _ensureInboxSeqCursorLoaded();
    final localStorageSummary = await LocalDb.getStorageSummary();
    final isFreshDeviceBootstrap =
        localStorageSummary.messageCount <= 0 && _lastInboxSeqCursor <= 0;
    if (!isFreshDeviceBootstrap) {
      return;
    }

    _observeBootstrapInboxSeqFloor(latestInboxSeq);
    _observeInboxSeq(latestInboxSeq);
  }

  Future<void> _handleAuthAckSuccess() async {
    _sendRealtimeAppStateIfPossible();

    // Route transitions are owned by splash/login/register flows. Keeping
    // auth_ack side effects local avoids Web-only navigation faults inside the
    // socket callback from breaking downstream message processing.
    await _runPostAuthSuccessStep(
      'ensure_deleted_sessions_loaded',
      _ensureDeletedSessionsLoaded,
    );
    await _runPostAuthSuccessStep(
      'sync_deleted_session_history_resets',
      _syncDeletedSessionHistoryResets,
    );
    await _runPostAuthSuccessStep(
      'ensure_pending_read_states_loaded',
      _ensurePendingReadStatesLoaded,
    );
    await _runPostAuthSuccessStep(
      'queue_deleted_session_read_clears',
      _queueDeletedSessionReadClears,
    );
    await _runPostAuthSuccessStep(
      'flush_pending_session_reads',
      _flushPendingSessionReads,
    );
    await _runPostAuthSuccessStep(
      'flush_pending_pull_sync',
      _flushPendingPullSync,
    );
    _runPostAuthSuccessStepInBackground(
      'sync_sessions_from_server',
      () => _syncSessionsFromServer(
        limit: ImService._coldStartSessionSnapshotLimit,
        maxPages: ImService._coldStartSessionSnapshotMaxPages,
        fullSync: false,
      ),
    );
    _runPostAuthSuccessStepInBackground('trigger_friend_sync', () async {
      _triggerFriendSync();
    });
    await _runPostAuthSuccessStep(
      'trigger_delegate_list',
      _triggerDelegateList,
    );
    await _runPostAuthSuccessStep(
      'resend_sending_messages_in_memory',
      _resendSendingMessagesInMemory,
    );
    _runPostAuthSuccessStepInBackground(
      'resend_pending_messages_from_db',
      _resendPendingMessagesFromDb,
    );
    await _runPostAuthSuccessStep(
      'restore_current_session_realtime_state',
      _restoreCurrentSessionRealtimeState,
    );
    _runPostAuthSuccessStepInBackground(
      'refresh_active_session_history_on_reconnect',
      refreshActiveSessionOnReconnect,
    );
  }

  Future<void> _runPostAuthSuccessStep(
    String step,
    FutureOr<void> Function() action,
  ) async {
    try {
      await action();
    } catch (e, st) {
      debugPrint('⚠️ auth_ack post step failed: $step err=$e\n$st');
    }
  }

  void _runPostAuthSuccessStepInBackground(
    String step,
    Future<void> Function() action,
  ) {
    unawaited(() async {
      try {
        await action();
      } catch (e, st) {
        debugPrint('⚠️ auth_ack post step failed: $step err=$e\n$st');
      }
    }());
  }

  int _resolveQueueLagMs(int? enqueuedAtMs) {
    if (enqueuedAtMs == null || enqueuedAtMs <= 0) return 0;
    final lag = DateTime.now().millisecondsSinceEpoch - enqueuedAtMs;
    if (lag <= 0) return 0;
    return lag;
  }

  void _logDownstreamLag(String cmd, int queueLagMs) {
    final key = cmd.trim().isEmpty ? 'unknown' : cmd.trim();
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final lastLogAtMs = _downstreamLagLastLogAtMsByCmd[key] ?? 0;
    if (lastLogAtMs > 0 &&
        nowMs - lastLogAtMs < ImService._downstreamLagLogIntervalMs) {
      _downstreamLagSuppressedByCmd[key] =
          (_downstreamLagSuppressedByCmd[key] ?? 0) + 1;
      return;
    }

    final suppressed = _downstreamLagSuppressedByCmd.remove(key) ?? 0;
    _downstreamLagLastLogAtMsByCmd[key] = nowMs;
    debugPrint(
      '⚠️ downstream lag cmd=$key queue=${queueLagMs}ms'
      '${suppressed > 0 ? ' suppressed=$suppressed' : ''}',
    );
  }

  Future<T?> _guardDbOp<T>(
    Future<T> future, {
    required String op,
    T? fallback,
  }) async {
    try {
      return await future.timeout(ImService._dbOpTimeout);
    } on TimeoutException {
      debugPrint(
        '⚠️ DB timeout op=$op after ${ImService._dbOpTimeout.inSeconds}s',
      );
      return fallback;
    } catch (e) {
      debugPrint('⚠️ DB error op=$op err=$e');
      return fallback;
    }
  }

  /// 执行写库操作并返回是否确实落盘成功。超时/异常返回 false。
  ///
  /// 用于 inbox_seq 游标推进的前置判断：只有当消息真正落盘成功，才允许推进
  /// 游标。若落盘失败仍推进游标，pull_sync 会因 `inbox_seq > cursor` 而永久
  /// 跳过这条消息，导致丢消息（仅能依赖会话级 HTTP 回填兜底，对活跃会话失效）。
  Future<bool> _guardDbWrite(
    Future<void> Function() action, {
    required String op,
  }) async {
    try {
      if (ImService.failDbWriteOpForTest?.call(op) ?? false) {
        debugPrint('⚠️ DB write injected failure op=$op');
        return false;
      }
      await action().timeout(ImService._dbOpTimeout);
      return true;
    } on TimeoutException {
      debugPrint(
        '⚠️ DB write timeout op=$op after ${ImService._dbOpTimeout.inSeconds}s',
      );
      return false;
    } catch (e) {
      debugPrint('⚠️ DB write error op=$op err=$e');
      return false;
    }
  }

  void _suppressIncomingMessage({
    required String msgId,
    required String sessionId,
    required String reason,
    int? queueLagMs,
  }) {
    if (msgId.isEmpty) {
      return;
    }
    _activeStreamingMsgIds.remove(msgId);
    _discardStreamingSessionPreview(msgId);
    _clearStreamChunkGapTrackingForMessage(msgId);
    _streamDiagFinalize(
      msgId,
      reason: reason,
      finalContent: '',
      queueLagMs: queueLagMs,
    );
    MessageStreamController.finish(msgId, '');
    _streamingPlaceholders.remove(msgId);
    if (_isCurrentSession(sessionId)) {
      _removeUIMessage(msgId);
    }
  }

  bool _shouldSuppressIncomingMessageMap(Map<String, dynamic> message) {
    final content = message['content']?.toString() ?? '';
    if (content.isEmpty) {
      return false;
    }
    final senderType = _toInt(message['sender_type']);
    return _shouldSuppressIncomingMessage(
      content: content,
      senderType: senderType,
    );
  }

  bool _shouldSuppressIncomingMessage({
    required String content,
    required int senderType,
  }) {
    if (content.isEmpty || senderType != 2) {
      return false;
    }
    return _isOpenClawToolGateErrorContent(content);
  }

  bool _isOpenClawToolGateErrorContent(String content) {
    final normalized = content.trimLeft();
    if (normalized.isEmpty) {
      return false;
    }
    final lower = normalized.toLowerCase();
    if (!lower.contains(ImService._openClawElevatedUnavailableMarker)) {
      return false;
    }
    if (lower.contains(ImService._openClawToolGateMarker)) {
      return true;
    }
    return lower.contains(ImService._openClawFixKeysMarker);
  }

  void _streamDiagOnChunk({
    required String msgId,
    required String sessionId,
    required String chunk,
    required int chunkSeq,
    required int queueLagMs,
  }) {
    if (!_streamDiagEnabled || msgId.isEmpty) return;
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final stats = _streamDiagByMsgId.putIfAbsent(msgId, () {
      debugPrint(
        '🧪 stream_diag start msg=$msgId sid=$sessionId first_queue=${queueLagMs}ms',
      );
      return _StreamDiagStats(sessionId: sessionId, createdAtMs: nowMs);
    });

    stats.chunkCount += 1;
    stats.totalChars += chunk.length;
    stats.queueLagSamples += 1;
    stats.queueLagTotalMs += queueLagMs;
    if (stats.firstQueueLagMs == 0) {
      stats.firstQueueLagMs = queueLagMs;
    }
    if (queueLagMs > stats.maxQueueLagMs) {
      stats.maxQueueLagMs = queueLagMs;
    }
    if (stats.firstChunkAtMs == 0) {
      stats.firstChunkAtMs = nowMs;
      stats.lastChunkAtMs = nowMs;
    } else {
      final gap = nowMs - stats.lastChunkAtMs;
      if (gap > stats.maxInterChunkGapMs) {
        stats.maxInterChunkGapMs = gap;
      }
      stats.lastChunkAtMs = nowMs;
    }

    if (queueLagMs >= 120) {
      debugPrint(
        '🧪 stream_diag lag msg=$msgId queue=${queueLagMs}ms chunk_seq=$chunkSeq chunks=${stats.chunkCount}',
      );
    }
    if (stats.chunkCount % 30 == 0) {
      debugPrint(
        '🧪 stream_diag progress msg=$msgId chunks=${stats.chunkCount} chars=${stats.totalChars} max_queue=${stats.maxQueueLagMs}ms',
      );
    }
  }

  void _onStreamUiUpdatedImpl(String msgId) {
    if (!_streamDiagEnabled || msgId.isEmpty) return;
    final stats = _streamDiagByMsgId[msgId];
    if (stats == null) return;
    final nowMs = DateTime.now().millisecondsSinceEpoch;
    stats.uiUpdateCount += 1;
    if (stats.firstUiAtMs == 0) {
      stats.firstUiAtMs = nowMs;
      final baseMs = stats.firstChunkAtMs > 0 ? stats.firstChunkAtMs : nowMs;
      final firstUiLagMs = nowMs - baseMs;
      debugPrint('🧪 stream_diag first_ui msg=$msgId lag=${firstUiLagMs}ms');
    }
  }

  void _streamDiagFinalize(
    String msgId, {
    required String reason,
    String? finalContent,
    int? queueLagMs,
  }) {
    if (!_streamDiagEnabled || msgId.isEmpty) return;
    final stats = _streamDiagByMsgId.remove(msgId);
    if (stats == null) return;

    final nowMs = DateTime.now().millisecondsSinceEpoch;
    final streamEndMs = stats.lastChunkAtMs > 0 ? stats.lastChunkAtMs : nowMs;
    final streamDurationMs = stats.firstChunkAtMs > 0
        ? (streamEndMs - stats.firstChunkAtMs).clamp(0, 1 << 30)
        : 0;
    final totalDurationMs = (nowMs - stats.createdAtMs).clamp(0, 1 << 30);
    final avgQueueMs = stats.queueLagSamples > 0
        ? stats.queueLagTotalMs ~/ stats.queueLagSamples
        : 0;
    var maxQueueMs = stats.maxQueueLagMs;
    if (queueLagMs != null && queueLagMs > maxQueueMs) {
      maxQueueMs = queueLagMs;
    }
    final firstUiLagMs = (stats.firstUiAtMs > 0 && stats.firstChunkAtMs > 0)
        ? (stats.firstUiAtMs - stats.firstChunkAtMs).clamp(0, 1 << 30)
        : -1;
    final finalChars = finalContent?.length ?? 0;

    debugPrint(
      '🧪 stream_diag summary msg=$msgId sid=${stats.sessionId} reason=$reason '
      'chunks=${stats.chunkCount} chars=${stats.totalChars} final_chars=$finalChars '
      'first_queue=${stats.firstQueueLagMs}ms avg_queue=${avgQueueMs}ms max_queue=${maxQueueMs}ms '
      'max_chunk_gap=${stats.maxInterChunkGapMs}ms first_ui='
      '${firstUiLagMs >= 0 ? '${firstUiLagMs}ms' : 'n/a'} '
      'ui_updates=${stats.uiUpdateCount} stream=${streamDurationMs}ms total=${totalDurationMs}ms',
    );
  }

  void _clearStreamDiagnostics({required String reason}) {
    if (_streamDiagByMsgId.isEmpty) return;
    if (!_streamDiagEnabled) {
      _streamDiagByMsgId.clear();
      return;
    }
    final pendingMsgIds = _streamDiagByMsgId.keys.toList(growable: false);
    for (final msgId in pendingMsgIds) {
      _streamDiagFinalize(msgId, reason: reason);
    }
  }

  /// 触发全局审批卡片横幅通知
  void _notifyInAppCardBanner({
    required String sessionId,
    required String sessionType,
    required ChatExecApprovalCardData approvalCard,
  }) {
    if (!Get.isRegistered<InAppNotificationService>()) return;
    final service = Get.find<InAppNotificationService>();

    service.show(
      sessionId: sessionId,
      sessionType: sessionType,
      title: 'notification_approval_request'.tr,
      summary: approvalCard.displayCommand,
    );
  }
}
