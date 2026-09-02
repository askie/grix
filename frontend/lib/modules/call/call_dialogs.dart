import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'call_controller.dart';
import 'call_state.dart';
import '../../data/providers/agent_service.dart';
import '../../data/providers/im_service.dart';

// ─────────────────────────────────────────────
// 来电弹窗（Phase 2：真人 / AI 二选一）
// ─────────────────────────────────────────────

/// 来电弹窗：显示来电信息，提供"真人接听"和"AI 代接"两个入口。
class IncomingCallDialog extends StatelessWidget {
  const IncomingCallDialog({super.key});

  static void show() {
    // 来电界面出现时隐藏键盘，避免软键盘遮挡来电操作
    FocusManager.instance.primaryFocus?.unfocus();
    Get.dialog(
      const IncomingCallDialog(),
      barrierDismissible: false,
      barrierColor: Colors.black87,
      useSafeArea: false,
    );
  }

  @override
  Widget build(BuildContext context) {
    final ctrl = Get.find<CallController>();
    final im = Get.find<ImService>();

    return Scaffold(
      backgroundColor: Colors.transparent,
      body: DefaultTextStyle(
        style: const TextStyle(decoration: TextDecoration.none),
        child: Container(
          color: Colors.black87,
          child: SafeArea(
            child: Obx(() {
              final session = ctrl.session;
              if (session == null) return const SizedBox.shrink();

              return Column(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Padding(
                    padding: const EdgeInsets.only(top: 12, right: 12),
                    child: Align(
                      alignment: Alignment.centerRight,
                      child: IconButton(
                        tooltip: MaterialLocalizations.of(
                          context,
                        ).closeButtonTooltip,
                        onPressed: () {
                          ctrl.dismissIncoming();
                          Get.back();
                        },
                        icon: const Icon(Icons.close, color: Colors.white70),
                      ),
                    ),
                  ),
                  // 来电信息
                  Column(
                    children: [
                      const Icon(
                        Icons.account_circle,
                        size: 100,
                        color: Colors.white54,
                      ),
                      const SizedBox(height: 16),
                      Text(
                        session.peerName.isNotEmpty
                            ? session.peerName
                            : 'call_unknown_caller'.tr,
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 28,
                          fontWeight: FontWeight.w500,
                        ),
                      ),
                      const SizedBox(height: 8),
                      Text(
                        'call_incoming_voice'.tr,
                        style: const TextStyle(
                          color: Colors.white54,
                          fontSize: 16,
                        ),
                      ),
                    ],
                  ),
                  // 操作按钮区
                  Padding(
                    padding: const EdgeInsets.only(bottom: 60),
                    child: Row(
                      mainAxisAlignment: MainAxisAlignment.spaceEvenly,
                      children: [
                        _CallButton(
                          icon: Icons.call_end,
                          color: Colors.red,
                          label: 'call_reject'.tr,
                          onTap: () {
                            ctrl.reject(im.sendCallPacket);
                            Get.back();
                          },
                        ),
                        _CallButton(
                          icon: Icons.call,
                          color: Colors.green,
                          label: 'call_answer'.tr,
                          onTap: () async {
                            await ctrl.answer(im.sendCallPacket);
                            final state = ctrl.session?.state;
                            if (state != CallState.connecting &&
                                state != CallState.active) {
                              return;
                            }
                            // 关闭来电弹窗，活跃通话 overlay 由 GrixApp 自动展示
                            Get.back();
                          },
                        ),
                        _CallButton(
                          icon: Icons.smart_toy_outlined,
                          color: Colors.blueAccent,
                          label: 'call_answer_with_ai'.tr,
                          onTap: () {
                            Get.back();
                            AgentPickerDialog.show();
                          },
                        ),
                      ],
                    ),
                  ),
                ],
              );
            }),
          ),
        ),
      ),
    );
  }
}

// ─────────────────────────────────────────────
// Agent 选择弹窗（Phase 2）
// ─────────────────────────────────────────────

/// AgentPickerDialog：列出支持语音的 agent，B 选择后触发 AI 代接。
class AgentPickerDialog extends StatelessWidget {
  const AgentPickerDialog({super.key});

  static void show() {
    Get.dialog(
      const AgentPickerDialog(),
      barrierDismissible: true,
      barrierColor: Colors.black54,
    );
  }

  @override
  Widget build(BuildContext context) {
    final ctrl = Get.find<CallController>();
    final im = Get.find<ImService>();
    final agentSvc = Get.find<AgentService>();

    return Dialog(
      backgroundColor: const Color(0xFF1E1E2E),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
      child: DefaultTextStyle(
        style: const TextStyle(decoration: TextDecoration.none),
        child: Padding(
          padding: const EdgeInsets.all(20),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                'call_pick_ai_agent'.tr,
                style: const TextStyle(
                  color: Colors.white,
                  fontSize: 18,
                  fontWeight: FontWeight.w600,
                ),
              ),
              const SizedBox(height: 4),
              Text(
                'call_pick_ai_agent_hint'.tr,
                style: const TextStyle(color: Colors.white54, fontSize: 13),
              ),
              const SizedBox(height: 16),
              Obx(() {
                final voiceAgents = agentSvc.agents
                    .where((a) => a.supportsVoice)
                    .toList();
                if (voiceAgents.isEmpty) {
                  return Padding(
                    padding: const EdgeInsets.symmetric(vertical: 16),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Text(
                          'call_no_voice_agent'.tr,
                          style: const TextStyle(color: Colors.white54),
                        ),
                        const SizedBox(height: 12),
                        TextButton.icon(
                          onPressed: () {
                            Get.back(); // 关闭 AgentPickerDialog
                            Get.back(); // 关闭来电弹窗
                            Get.toNamed(
                              '/agent/create',
                              arguments: {'preset_provider_type': 4},
                            );
                          },
                          icon: const Icon(
                            Icons.add,
                            color: Colors.blueAccent,
                            size: 18,
                          ),
                          label: Text(
                            'call_create_voice_agent'.tr,
                            style: const TextStyle(
                              color: Colors.blueAccent,
                              fontSize: 13,
                            ),
                          ),
                        ),
                      ],
                    ),
                  );
                }
                return ConstrainedBox(
                  constraints: const BoxConstraints(maxHeight: 320),
                  child: ListView.separated(
                    shrinkWrap: true,
                    itemCount: voiceAgents.length,
                    separatorBuilder: (_, __) =>
                        const Divider(color: Colors.white12, height: 1),
                    itemBuilder: (_, i) {
                      final agent = voiceAgents[i];
                      return ListTile(
                        contentPadding: EdgeInsets.zero,
                        leading: CircleAvatar(
                          backgroundColor: Colors.white12,
                          backgroundImage: agent.avatarUrl.isNotEmpty
                              ? NetworkImage(agent.avatarUrl)
                              : null,
                          child: agent.avatarUrl.isEmpty
                              ? const Icon(
                                  Icons.smart_toy,
                                  color: Colors.white54,
                                )
                              : null,
                        ),
                        title: Text(
                          agent.agentName,
                          style: const TextStyle(
                            color: Colors.white,
                            fontSize: 15,
                          ),
                        ),
                        subtitle: agent.introduction.isNotEmpty
                            ? Text(
                                agent.introduction,
                                style: const TextStyle(
                                  color: Colors.white54,
                                  fontSize: 12,
                                ),
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                              )
                            : null,
                        trailing: const Icon(
                          Icons.chevron_right,
                          color: Colors.white38,
                        ),
                        onTap: () {
                          Get.back();
                          ctrl.answerWithAI(
                            agent.id,
                            agent.agentName,
                            im.sendCallPacket,
                          );
                          // 活跃通话 overlay 由 GrixApp 自动展示
                        },
                      );
                    },
                  ),
                );
              }),
              const SizedBox(height: 8),
              Align(
                alignment: Alignment.centerRight,
                child: TextButton(
                  onPressed: Get.back,
                  child: Text(
                    'cancel'.tr,
                    style: const TextStyle(color: Colors.white54),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

// ─────────────────────────────────────────────
// 通话中全屏 overlay（由 GrixApp builder Stack 渲染）
// ─────────────────────────────────────────────

/// 通话中全屏界面：在 GrixApp builder 的 Stack 中以 Positioned.fill 展示。
/// 所有状态（计时、静音、扬声器）均读取自 CallController，折叠/展开不丢失。
class ActiveCallOverlay extends StatefulWidget {
  const ActiveCallOverlay({super.key});

  @override
  State<ActiveCallOverlay> createState() => _ActiveCallOverlayState();
}

class _ActiveCallOverlayState extends State<ActiveCallOverlay> {
  @override
  void initState() {
    super.initState();
    // 通话全屏界面出现时隐藏键盘
    FocusManager.instance.primaryFocus?.unfocus();
  }

  @override
  Widget build(BuildContext context) {
    final ctrl = Get.find<CallController>();
    final im = Get.find<ImService>();

    return DefaultTextStyle(
      style: const TextStyle(decoration: TextDecoration.none),
      child: Obx(() {
        final session = ctrl.session;
        if (session == null) return const SizedBox.shrink();
        final muted = ctrl.isMuted.value;
        final speakerOn = ctrl.isSpeakerOn.value;
        // 读 callMode（内部读取 isStandby/state/isMuted，使 Obx 跟踪待命态变化）
        final mode = ctrl.callMode;
        final ownerDelegated = !session.isCaller && session.isAIInvolved;
        // 麦克风仅在「需要开麦」的档显示（加入/接管），或非主人代接场景
        final showMic =
            !ownerDelegated ||
            mode == CallMode.joined ||
            mode == CallMode.takeover;
        // 扬声器在旁听档也显示：旁听已连房在听，可切扬声器/听筒
        final showSpeaker = showMic || mode == CallMode.listening;

        // 操作按钮：静音/扬声器（仅需开麦档）+ 离开（代接/接管）+ 挂断。
        // 全屏布局与紧凑卡片共用同一组按钮，按钮间统一插 28 间距。
        final actions = <Widget>[
          if (session.state != CallState.queued && showMic)
            _CallActionButton(
              icon: muted ? Icons.mic_off : Icons.mic,
              label: muted ? 'call_unmute'.tr : 'call_mute'.tr,
              background: muted ? Colors.white24 : Colors.white12,
              onTap: () {
                ctrl.isMuted.value = !ctrl.isMuted.value;
                ctrl.setMuted(ctrl.isMuted.value);
              },
            ),
          if (session.state != CallState.queued && showSpeaker)
            _CallActionButton(
              icon: speakerOn ? Icons.volume_up : Icons.volume_down,
              label: 'call_speaker'.tr,
              background: speakerOn ? Colors.white24 : Colors.white12,
              onTap: () {
                ctrl.isSpeakerOn.value = !ctrl.isSpeakerOn.value;
                ctrl.setSpeakerOn(ctrl.isSpeakerOn.value);
              },
            ),
          // AI 代接/接管中额外显示"离开"（交回 AI，不挂断）
          // 直拨 AI 时 owner 是主叫方(isCaller=true)，无需离开按钮
          if (!session.isCaller &&
              (session.state == CallState.aiDelegated ||
                  session.state == CallState.humanActive))
            _CallActionButton(
              icon: Icons.logout,
              label: 'call_leave'.tr,
              background: Colors.white24,
              onTap: () => ctrl.leaveCall(im.sendCallPacket),
            ),
          _CallActionButton(
            icon: Icons.call_end,
            label: 'call_hangup'.tr,
            background: Colors.red,
            onTap: () => ctrl.hangup(im.sendCallPacket),
          ),
        ];
        final actionRow = <Widget>[];
        for (var i = 0; i < actions.length; i++) {
          if (i > 0) actionRow.add(const SizedBox(width: 28));
          actionRow.add(actions[i]);
        }

        // 自己直拨 AI（语音大脑 / 语音 Agent 测试拨打）：内容很少，
        // 用屏幕中央的小卡片而非全屏铺满。真人通话/被叫代接仍走原全屏布局。
        final compact =
            session.isCaller &&
            session.delegationMode == DelegationMode.aiDelegated;
        if (compact) {
          return _CompactCallCard(
            session: session,
            ctrl: ctrl,
            mode: mode,
            actionRow: actionRow,
          );
        }

        return Container(
          color: Colors.black87,
          child: SafeArea(
            child: Column(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                // 顶部：折叠按钮
                Align(
                  alignment: Alignment.topRight,
                  child: Padding(
                    padding: const EdgeInsets.only(top: 8, right: 12),
                    child: IconButton(
                      onPressed: ctrl.minimize,
                      icon: const Icon(
                        Icons.keyboard_arrow_down,
                        color: Colors.white70,
                        size: 32,
                      ),
                      tooltip: 'call_minimize'.tr,
                    ),
                  ),
                ),
                // 通话信息 + AI 状态标签
                Column(
                  children: [
                    const Icon(
                      Icons.account_circle,
                      size: 100,
                      color: Colors.white54,
                    ),
                    const SizedBox(height: 16),
                    Text(
                      session.peerName.isNotEmpty
                          ? session.peerName
                          : 'call_unknown_caller'.tr,
                      style: const TextStyle(
                        color: Colors.white,
                        fontSize: 28,
                        fontWeight: FontWeight.w500,
                      ),
                    ),
                    const SizedBox(height: 8),
                    _CallTimer(
                      stopwatch: ctrl.callStopwatch,
                      state: session.state,
                      connectingPhase: session.connectingPhase,
                      queuePosition: session.queuePosition,
                    ),
                    const SizedBox(height: 12),
                    _AiStatusBadge(session: session, mode: mode),
                  ],
                ),
                // 控制按钮
                Padding(
                  padding: const EdgeInsets.only(bottom: 60),
                  child: Column(
                    children: [
                      if (session.state != CallState.queued)
                        // 主人侧四档选择器（待命/旁听/加入/接管）；其余场景渲染为空
                        _CallModeSelector(session: session, ctrl: ctrl, im: im),
                      const SizedBox(height: 32),
                      Row(
                        mainAxisSize: MainAxisSize.min,
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: actionRow,
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
        );
      }),
    );
  }
}

/// 紧凑通话卡片：自己直拨 AI（语音大脑 / 语音 Agent 测试拨打）时使用。
/// 这类通话只有状态文字和挂断键，内容很少，铺满全屏显得又大又空，
/// 改为屏幕中央的小卡片 + 半透明压暗背景。
class _CompactCallCard extends StatelessWidget {
  final CallSession session;
  final CallController ctrl;
  final CallMode mode;
  final List<Widget> actionRow;

  const _CompactCallCard({
    required this.session,
    required this.ctrl,
    required this.mode,
    required this.actionRow,
  });

  @override
  Widget build(BuildContext context) {
    return Container(
      color: Colors.black54,
      child: SafeArea(
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 340),
            child: Container(
              margin: const EdgeInsets.symmetric(horizontal: 24),
              padding: const EdgeInsets.fromLTRB(24, 12, 24, 28),
              decoration: BoxDecoration(
                color: const Color(0xFF1E1E1E),
                borderRadius: BorderRadius.circular(20),
              ),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  // 顶部：收起按钮（折叠为顶部横幅）
                  Align(
                    alignment: Alignment.topRight,
                    child: GestureDetector(
                      onTap: ctrl.minimize,
                      child: const Icon(
                        Icons.keyboard_arrow_down,
                        color: Colors.white70,
                        size: 26,
                      ),
                    ),
                  ),
                  const Icon(
                    Icons.account_circle,
                    size: 64,
                    color: Colors.white54,
                  ),
                  const SizedBox(height: 12),
                  Text(
                    session.peerName.isNotEmpty
                        ? session.peerName
                        : 'call_unknown_caller'.tr,
                    textAlign: TextAlign.center,
                    style: const TextStyle(
                      color: Colors.white,
                      fontSize: 20,
                      fontWeight: FontWeight.w500,
                    ),
                  ),
                  const SizedBox(height: 6),
                  _CallTimer(
                    stopwatch: ctrl.callStopwatch,
                    state: session.state,
                    connectingPhase: session.connectingPhase,
                    queuePosition: session.queuePosition,
                  ),
                  const SizedBox(height: 10),
                  _AiStatusBadge(session: session, mode: mode),
                  const SizedBox(height: 24),
                  // 窄屏时按钮行可能超出卡片宽度，用 FittedBox 自动缩放防溢出。
                  FittedBox(
                    fit: BoxFit.scaleDown,
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: actionRow,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

// ─────────────────────────────────────────────
// 折叠通话横幅（由 GrixApp builder Stack 渲染）
// ─────────────────────────────────────────────

/// 折叠通话横幅：紧凑显示在屏幕顶部，包含挂断和展开按钮。
class CollapsedCallBanner extends StatelessWidget {
  const CollapsedCallBanner({super.key});

  @override
  Widget build(BuildContext context) {
    final ctrl = Get.find<CallController>();
    final im = Get.find<ImService>();

    return Material(
      color: Colors.transparent,
      child: DefaultTextStyle(
        style: const TextStyle(decoration: TextDecoration.none),
        child: Obx(() {
          final session = ctrl.session;
          if (session == null) return const SizedBox.shrink();

          return Container(
            height: 48,
            padding: const EdgeInsets.symmetric(horizontal: 12),
            decoration: BoxDecoration(
              color: const Color(0xFF1A73E8),
              borderRadius: BorderRadius.circular(12),
              boxShadow: [
                BoxShadow(
                  color: Colors.black.withValues(alpha: 0.2),
                  blurRadius: 10,
                  offset: const Offset(0, 4),
                ),
              ],
            ),
            child: Row(
              children: [
                // 通话图标 + 计时脉冲
                const _CallPulseIcon(),
                const SizedBox(width: 8),
                // 对方名称 + 计时
                Expanded(
                  child: Column(
                    mainAxisAlignment: MainAxisAlignment.center,
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        session.peerName.isNotEmpty
                            ? session.peerName
                            : 'call_unknown_caller'.tr,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(
                          color: Colors.white,
                          fontSize: 13,
                          fontWeight: FontWeight.w600,
                        ),
                      ),
                      _CallTimerText(
                        stopwatch: ctrl.callStopwatch,
                        state: session.state,
                        connectingPhase: session.connectingPhase,
                        queuePosition: session.queuePosition,
                      ),
                    ],
                  ),
                ),
                const SizedBox(width: 8),
                // 展开按钮
                GestureDetector(
                  onTap: ctrl.expand,
                  child: Container(
                    width: 36,
                    height: 36,
                    decoration: BoxDecoration(
                      color: Colors.white.withValues(alpha: 0.2),
                      shape: BoxShape.circle,
                    ),
                    child: const Icon(
                      Icons.fullscreen,
                      color: Colors.white,
                      size: 20,
                    ),
                  ),
                ),
                const SizedBox(width: 8),
                // 挂断按钮
                GestureDetector(
                  onTap: () => ctrl.hangup(im.sendCallPacket),
                  child: Container(
                    width: 36,
                    height: 36,
                    decoration: const BoxDecoration(
                      color: Colors.red,
                      shape: BoxShape.circle,
                    ),
                    child: const Icon(
                      Icons.call_end,
                      color: Colors.white,
                      size: 18,
                    ),
                  ),
                ),
              ],
            ),
          );
        }),
      ),
    );
  }
}

// ─────────────────────────────────────────────
// Phase 2 子组件

/// AI 状态标签：显示当前是 AI 托管中 / 真人接管中 / 真人通话
class _AiStatusBadge extends StatelessWidget {
  final CallSession session;

  /// 主人当前所处的参与档，用于让状态标签与四档按钮联动。
  final CallMode mode;
  const _AiStatusBadge({required this.session, required this.mode});

  @override
  Widget build(BuildContext context) {
    // 主人(被叫)在 AI 代接通话里：状态标签随四档（待命/旁听/加入/接管）联动。
    if (!session.isCaller && session.isAIInvolved) {
      switch (mode) {
        case CallMode.standby:
          return _badge(
            icon: Icons.pause_circle_outline,
            label: _withAgent('call_standby_status'.tr),
            color: Colors.blueGrey,
          );
        case CallMode.listening:
          return Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              _badge(
                icon: Icons.hearing,
                label: _withAgent('call_listening'.tr),
                color: Colors.tealAccent,
              ),
              const SizedBox(height: 6),
              Text(
                'call_listening_hint'.tr,
                style: const TextStyle(color: Colors.white38, fontSize: 12),
              ),
            ],
          );
        case CallMode.joined:
          return _badge(
            icon: Icons.groups,
            label: _withAgent('call_joined_status'.tr),
            color: Colors.greenAccent,
          );
        case CallMode.takeover:
          return _badge(
            icon: Icons.person,
            label: 'call_human_active'.tr,
            color: Colors.orangeAccent,
          );
      }
    }

    // visitor（主叫）/ 普通真人通话：按通话底层状态显示。
    switch (session.state) {
      case CallState.aiDelegated:
        return _badge(
          icon: Icons.smart_toy,
          label: _withAgent('call_ai_answering'.tr),
          color: Colors.blueAccent,
        );
      case CallState.humanActive:
        return _badge(
          icon: Icons.person,
          label: 'call_human_active'.tr,
          color: Colors.orangeAccent,
        );
      default:
        return const SizedBox.shrink();
    }
  }

  /// AI 相关档位的标签追加 agent 名称（接管时无 AI 应答，不追加）。
  String _withAgent(String base) =>
      session.agentName != null ? '$base · ${session.agentName}' : base;

  Widget _badge({
    required IconData icon,
    required String label,
    required Color color,
  }) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.15),
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: color.withValues(alpha: 0.4)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, color: color, size: 16),
          const SizedBox(width: 6),
          Text(label, style: TextStyle(color: color, fontSize: 13)),
        ],
      ),
    );
  }
}

/// 接管 / 交回按钮行（仅在 AI 托管相关状态下显示）
class _CallModeSelector extends StatelessWidget {
  final CallSession session;
  final CallController ctrl;
  final ImService im;

  const _CallModeSelector({
    required this.session,
    required this.ctrl,
    required this.im,
  });

  @override
  Widget build(BuildContext context) {
    // 仅主人(被叫)在 AI 代接的通话里显示四档；visitor / 普通真人通话不显示
    if (session.isCaller || !session.isAIInvolved) {
      return const SizedBox.shrink();
    }
    final current = ctrl.callMode;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: Row(
        children: [
          _seg(
            current,
            CallMode.standby,
            Icons.pause_circle_outline,
            'call_mode_standby'.tr,
          ),
          _seg(
            current,
            CallMode.listening,
            Icons.hearing,
            'call_mode_listening'.tr,
          ),
          _seg(current, CallMode.joined, Icons.groups, 'call_mode_joined'.tr),
          _seg(
            current,
            CallMode.takeover,
            Icons.person_pin,
            'call_mode_takeover'.tr,
          ),
        ],
      ),
    );
  }

  Widget _seg(CallMode current, CallMode m, IconData icon, String label) {
    final active = current == m;
    return Expanded(
      child: GestureDetector(
        onTap: () => ctrl.setCallMode(m, im.sendCallPacket),
        child: Container(
          margin: const EdgeInsets.symmetric(horizontal: 4),
          padding: const EdgeInsets.symmetric(vertical: 10),
          decoration: BoxDecoration(
            color: active ? Colors.white : Colors.white12,
            borderRadius: BorderRadius.circular(16),
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                icon,
                size: 22,
                color: active ? Colors.black87 : Colors.white70,
              ),
              const SizedBox(height: 4),
              Text(
                label,
                style: TextStyle(
                  fontSize: 12,
                  color: active ? Colors.black87 : Colors.white70,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

// ─────────────────────────────────────────────
// 公共子组件
// ─────────────────────────────────────────────

class _CallButton extends StatelessWidget {
  final IconData icon;
  final Color color;
  final String label;
  final VoidCallback onTap;

  const _CallButton({
    required this.icon,
    required this.color,
    required this.label,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        GestureDetector(
          onTap: onTap,
          child: Container(
            width: 72,
            height: 72,
            decoration: BoxDecoration(color: color, shape: BoxShape.circle),
            child: Icon(icon, color: Colors.white, size: 36),
          ),
        ),
        const SizedBox(height: 8),
        Text(
          label,
          style: const TextStyle(color: Colors.white70, fontSize: 14),
        ),
      ],
    );
  }
}

class _CallTimer extends StatefulWidget {
  final Stopwatch stopwatch;
  final CallState state;
  final ConnectingPhase? connectingPhase;
  final int? queuePosition;
  const _CallTimer({
    required this.stopwatch,
    required this.state,
    this.connectingPhase,
    this.queuePosition,
  });

  @override
  State<_CallTimer> createState() => _CallTimerState();
}

class _CallTimerState extends State<_CallTimer> {
  late final Stream<int> _ticker;

  @override
  void initState() {
    super.initState();
    _ticker = Stream.periodic(const Duration(seconds: 1), (i) => i);
  }

  @override
  Widget build(BuildContext context) {
    return StreamBuilder<int>(
      stream: _ticker,
      builder: (_, __) {
        if (widget.state == CallState.queued) {
          final pos = widget.queuePosition ?? 1;
          return Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(
                  strokeWidth: 2,
                  color: Colors.white54,
                ),
              ),
              const SizedBox(width: 8),
              Text(
                '${'call_queue_waiting'.tr} ${'call_queue_position'.trParams({'pos': pos.toString()})}',
                style: const TextStyle(color: Colors.white54, fontSize: 16),
              ),
            ],
          );
        }
        if (widget.state == CallState.connecting) {
          final label = switch (widget.connectingPhase) {
            ConnectingPhase.launching => 'call_connecting_launching'.tr,
            ConnectingPhase.waiting => 'call_connecting_waiting'.tr,
            null => 'call_connecting'.tr,
          };
          return Text(
            label,
            style: const TextStyle(color: Colors.white54, fontSize: 16),
          );
        }
        final elapsed = widget.stopwatch.elapsed;
        final mm = elapsed.inMinutes.toString().padLeft(2, '0');
        final ss = (elapsed.inSeconds % 60).toString().padLeft(2, '0');
        return Text(
          '$mm:$ss',
          style: const TextStyle(color: Colors.white54, fontSize: 16),
        );
      },
    );
  }
}

/// 横幅用的紧凑计时文本（不使用 StreamBuilder，直接每秒刷新）
class _CallTimerText extends StatefulWidget {
  final Stopwatch stopwatch;
  final CallState state;
  final ConnectingPhase? connectingPhase;
  final int? queuePosition;
  const _CallTimerText({
    required this.stopwatch,
    required this.state,
    this.connectingPhase,
    this.queuePosition,
  });

  @override
  State<_CallTimerText> createState() => _CallTimerTextState();
}

class _CallTimerTextState extends State<_CallTimerText> {
  late final Stream<int> _ticker;

  @override
  void initState() {
    super.initState();
    _ticker = Stream.periodic(const Duration(seconds: 1), (i) => i);
  }

  @override
  Widget build(BuildContext context) {
    return StreamBuilder<int>(
      stream: _ticker,
      builder: (_, __) {
        if (widget.state == CallState.queued) {
          final pos = widget.queuePosition ?? 1;
          return Text(
            '${'call_queue_waiting'.tr} #$pos',
            style: const TextStyle(color: Colors.white70, fontSize: 11),
          );
        }
        if (widget.state == CallState.connecting) {
          final label = switch (widget.connectingPhase) {
            ConnectingPhase.launching => 'call_connecting_launching'.tr,
            ConnectingPhase.waiting => 'call_connecting_waiting'.tr,
            null => 'call_connecting'.tr,
          };
          return Text(
            label,
            style: const TextStyle(color: Colors.white70, fontSize: 11),
          );
        }
        final elapsed = widget.stopwatch.elapsed;
        final mm = elapsed.inMinutes.toString().padLeft(2, '0');
        final ss = (elapsed.inSeconds % 60).toString().padLeft(2, '0');
        return Text(
          '$mm:$ss',
          style: const TextStyle(color: Colors.white70, fontSize: 11),
        );
      },
    );
  }
}

/// 横幅通话图标，带脉冲动画
class _CallPulseIcon extends StatefulWidget {
  const _CallPulseIcon();

  @override
  State<_CallPulseIcon> createState() => _CallPulseIconState();
}

class _CallPulseIconState extends State<_CallPulseIcon>
    with SingleTickerProviderStateMixin {
  late final AnimationController _controller;
  late final Animation<double> _scale;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1200),
    )..repeat(reverse: true);
    _scale = Tween<double>(
      begin: 1.0,
      end: 1.15,
    ).animate(CurvedAnimation(parent: _controller, curve: Curves.easeInOut));
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return ScaleTransition(
      scale: _scale,
      child: const Icon(Icons.call, color: Colors.white, size: 20),
    );
  }
}

/// 通话中底部操作圆钮：静音/扬声器/离开/挂断共用，确保尺寸、图标、文字完全一致。
class _CallActionButton extends StatelessWidget {
  final IconData icon;
  final String label;
  final Color background;
  final VoidCallback onTap;

  const _CallActionButton({
    required this.icon,
    required this.label,
    required this.background,
    required this.onTap,
  });

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        GestureDetector(
          onTap: onTap,
          child: Container(
            width: 60,
            height: 60,
            decoration: BoxDecoration(
              color: background,
              shape: BoxShape.circle,
            ),
            child: Icon(icon, color: Colors.white, size: 28),
          ),
        ),
        const SizedBox(height: 6),
        Text(
          label,
          style: const TextStyle(color: Colors.white70, fontSize: 12),
        ),
      ],
    );
  }
}
