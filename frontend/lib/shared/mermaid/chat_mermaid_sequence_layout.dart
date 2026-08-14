import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'chat_mermaid_model.dart';

class ChatMermaidSequenceLayout {
  const ChatMermaidSequenceLayout({
    required this.canvasSize,
    required this.participants,
    required this.events,
    required this.groups,
    required this.headerBottom,
    required this.lifelineBottom,
  });

  final Size canvasSize;
  final List<ChatMermaidSequenceParticipantLayout> participants;
  final List<ChatMermaidSequenceEventLayout> events;
  final List<ChatMermaidSequenceGroupLayout> groups;
  final double headerBottom;
  final double lifelineBottom;
}

class ChatMermaidSequenceParticipantLayout {
  const ChatMermaidSequenceParticipantLayout({
    required this.participant,
    required this.rect,
  });

  final ChatMermaidSequenceParticipant participant;
  final Rect rect;

  double get centerX => rect.center.dx;
}

abstract class ChatMermaidSequenceEventLayout {
  const ChatMermaidSequenceEventLayout({
    required this.event,
    required this.top,
    required this.height,
  });

  final ChatMermaidSequenceEvent event;
  final double top;
  final double height;

  double get bottom => top + height;
  double get centerY => top + (height / 2);
}

class ChatMermaidSequenceMessageLayout extends ChatMermaidSequenceEventLayout {
  const ChatMermaidSequenceMessageLayout({
    required super.event,
    required super.top,
    required super.height,
    required this.start,
    required this.end,
    required this.labelCenter,
    this.selfTurn = 0,
  });

  final Offset start;
  final Offset end;
  final Offset labelCenter;
  final int selfTurn;
}

class ChatMermaidSequenceNoteLayout extends ChatMermaidSequenceEventLayout {
  const ChatMermaidSequenceNoteLayout({
    required super.event,
    required super.top,
    required super.height,
    required this.rect,
  });

  final Rect rect;
}

class ChatMermaidSequenceDividerLayout extends ChatMermaidSequenceEventLayout {
  const ChatMermaidSequenceDividerLayout({
    required super.event,
    required super.top,
    required super.height,
    required this.left,
    required this.right,
  });

  final double left;
  final double right;
}

class ChatMermaidSequenceSpacerLayout extends ChatMermaidSequenceEventLayout {
  const ChatMermaidSequenceSpacerLayout({
    required super.event,
    required super.top,
    required super.height,
  });
}

class ChatMermaidSequenceGroupLayout {
  const ChatMermaidSequenceGroupLayout({
    required this.kind,
    required this.label,
    required this.depth,
    required this.rect,
    required this.dividers,
  });

  final ChatMermaidSequenceGroupKind kind;
  final String label;
  final int depth;
  final Rect rect;
  final List<ChatMermaidSequenceGroupDividerMarker> dividers;
}

class ChatMermaidSequenceGroupDividerMarker {
  const ChatMermaidSequenceGroupDividerMarker({
    required this.y,
    required this.label,
  });

  final double y;
  final String label;
}

class ChatMermaidSequenceLayoutEngine {
  const ChatMermaidSequenceLayoutEngine({
    this.padding = const EdgeInsets.fromLTRB(24, 20, 24, 20),
  });

  static const double _participantToLifelineGap = 34;

  final EdgeInsets padding;

  ChatMermaidSequenceLayout layout({
    required ChatMermaidSequenceDiagram diagram,
    required TextStyle participantStyle,
    required TextStyle messageStyle,
    required TextStyle noteStyle,
    required TextStyle groupStyle,
    required TextDirection textDirection,
  }) {
    final participants = _layoutParticipants(
      participants: diagram.participants,
      style: participantStyle,
      textDirection: textDirection,
    );
    final participantById = <String, ChatMermaidSequenceParticipantLayout>{
      for (final participant in participants)
        participant.participant.id: participant,
    };

    final headerBottom = participants.fold<double>(
          padding.top,
          (current, participant) => math.max(current, participant.rect.bottom),
        ) +
        _participantToLifelineGap;
    final baseLeft = participants.first.rect.left - 18;
    final baseRight = participants.last.rect.right + 18;
    final eventLayouts = <ChatMermaidSequenceEventLayout>[];
    final groups = <ChatMermaidSequenceGroupLayout>[];
    final groupStack = <_OpenGroup>[];

    var currentY = headerBottom;
    var selfTurn = 0;
    for (final event in diagram.events) {
      switch (event) {
        case ChatMermaidSequenceGroupStart():
          final top = currentY;
          currentY += 34;
          eventLayouts.add(
            ChatMermaidSequenceSpacerLayout(
              event: event,
              top: top,
              height: 34,
            ),
          );
          groupStack.add(
            _OpenGroup(
              kind: event.kind,
              label: event.label,
              top: top,
              depth: groupStack.length,
            ),
          );
          break;
        case ChatMermaidSequenceGroupDivider():
          final top = currentY;
          currentY += 30;
          eventLayouts.add(
            ChatMermaidSequenceDividerLayout(
              event: event,
              top: top,
              height: 30,
              left: baseLeft + (groupStack.length * 8),
              right: baseRight - (groupStack.length * 8),
            ),
          );
          if (groupStack.isNotEmpty) {
            groupStack.last.dividers.add(
              ChatMermaidSequenceGroupDividerMarker(
                y: top + 15,
                label: event.label,
              ),
            );
          }
          break;
        case ChatMermaidSequenceGroupEnd():
          final top = currentY;
          currentY += 10;
          eventLayouts.add(
            ChatMermaidSequenceSpacerLayout(
              event: event,
              top: top,
              height: 10,
            ),
          );
          if (groupStack.isNotEmpty) {
            final group = groupStack.removeLast();
            groups.add(
              ChatMermaidSequenceGroupLayout(
                kind: group.kind,
                label: group.label,
                depth: group.depth,
                rect: Rect.fromLTRB(
                  baseLeft + (group.depth * 8),
                  group.top,
                  baseRight - (group.depth * 8),
                  currentY,
                ),
                dividers: List.unmodifiable(group.dividers),
              ),
            );
          }
          break;
        case ChatMermaidSequenceNote():
          final noteRect = _layoutNote(
            event: event,
            participantById: participantById,
            top: currentY,
            style: noteStyle,
            textDirection: textDirection,
            baseLeft: baseLeft,
            baseRight: baseRight,
          );
          eventLayouts.add(
            ChatMermaidSequenceNoteLayout(
              event: event,
              top: currentY,
              height: noteRect.height + 16,
              rect: noteRect,
            ),
          );
          currentY += noteRect.height + 20;
          break;
        case ChatMermaidSequenceMessage():
          final source = participantById[event.fromId];
          final target = participantById[event.toId];
          if (source == null || target == null) {
            continue;
          }
          final isSelf = event.isSelfMessage;
          final top = currentY;
          final height = isSelf ? 58.0 : 44.0;
          final rowCenter = top + (height / 2);
          final layout = ChatMermaidSequenceMessageLayout(
            event: event,
            top: top,
            height: height,
            start: Offset(source.centerX, rowCenter),
            end: Offset(target.centerX, rowCenter),
            labelCenter: isSelf
                ? Offset(source.centerX + 48 + (selfTurn * 10), top + 12)
                : Offset((source.centerX + target.centerX) / 2, top + 12),
            selfTurn: isSelf ? selfTurn++ : 0,
          );
          eventLayouts.add(layout);
          currentY += height;
          if (!isSelf) {
            selfTurn = 0;
          }
          break;
      }
    }

    final canvasWidth = participants.last.rect.right + padding.right;
    final canvasHeight = currentY + padding.bottom;

    return ChatMermaidSequenceLayout(
      canvasSize: Size(canvasWidth, canvasHeight),
      participants: List.unmodifiable(participants),
      events: List.unmodifiable(eventLayouts),
      groups: List.unmodifiable(groups),
      headerBottom: headerBottom,
      lifelineBottom: canvasHeight - padding.bottom,
    );
  }

  List<ChatMermaidSequenceParticipantLayout> _layoutParticipants({
    required List<ChatMermaidSequenceParticipant> participants,
    required TextStyle style,
    required TextDirection textDirection,
  }) {
    final layouts = <ChatMermaidSequenceParticipantLayout>[];
    var currentLeft = padding.left;
    for (final participant in participants) {
      final textPainter = TextPainter(
        text: TextSpan(text: participant.label, style: style),
        textDirection: textDirection,
        maxLines: 2,
        ellipsis: '...',
      )..layout(maxWidth: 160);
      final width = math.max(88.0, textPainter.width + 28);
      final height = math.max(40.0, textPainter.height + 18);
      final rect = Rect.fromLTWH(currentLeft, padding.top, width, height);
      layouts.add(
        ChatMermaidSequenceParticipantLayout(
          participant: participant,
          rect: rect,
        ),
      );
      currentLeft = rect.right + 56;
    }
    return layouts;
  }

  Rect _layoutNote({
    required ChatMermaidSequenceNote event,
    required Map<String, ChatMermaidSequenceParticipantLayout> participantById,
    required double top,
    required TextStyle style,
    required TextDirection textDirection,
    required double baseLeft,
    required double baseRight,
  }) {
    final painter = TextPainter(
      text: TextSpan(text: event.text, style: style),
      textDirection: textDirection,
      maxLines: 4,
      ellipsis: '...',
    )..layout(maxWidth: 180);
    final width = math.max(104.0, painter.width + 22);
    final height = math.max(34.0, painter.height + 18);

    final anchors = event.targetIds
        .map((id) => participantById[id])
        .whereType<ChatMermaidSequenceParticipantLayout>()
        .toList(growable: false);
    if (anchors.isEmpty) {
      return Rect.fromLTWH(baseLeft, top + 8, width, height);
    }

    switch (event.position) {
      case ChatMermaidSequenceNotePosition.over:
        final left = anchors.first.centerX;
        final right = anchors.last.centerX;
        final centerX = (left + right) / 2;
        return Rect.fromCenter(
          center: Offset(centerX, top + 8 + (height / 2)),
          width: width,
          height: height,
        );
      case ChatMermaidSequenceNotePosition.leftOf:
        final anchor = anchors.first;
        final noteLeft = math.max(baseLeft, anchor.rect.left - width - 16);
        return Rect.fromLTWH(noteLeft, top + 8, width, height);
      case ChatMermaidSequenceNotePosition.rightOf:
        final anchor = anchors.last;
        final noteLeft = math.min(baseRight - width, anchor.rect.right + 16);
        return Rect.fromLTWH(noteLeft, top + 8, width, height);
    }
  }
}

class _OpenGroup {
  _OpenGroup({
    required this.kind,
    required this.label,
    required this.top,
    required this.depth,
  });

  final ChatMermaidSequenceGroupKind kind;
  final String label;
  final double top;
  final int depth;
  final List<ChatMermaidSequenceGroupDividerMarker> dividers =
      <ChatMermaidSequenceGroupDividerMarker>[];
}
