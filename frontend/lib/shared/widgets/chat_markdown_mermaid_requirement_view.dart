import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../mermaid/chat_mermaid_model.dart';

/// 需求图渲染:需求节点显示为卡片(含类型/id/text/risk/verifymethod),
/// 元素节点显示为轻量卡片(含 type/docref),下方列出关系。
class ChatMarkdownMermaidRequirementView extends StatelessWidget {
  const ChatMarkdownMermaidRequirementView({
    super.key,
    required this.diagram,
    required this.textStyle,
    required this.backgroundColor,
  });

  final ChatMermaidRequirementDiagram diagram;
  final TextStyle textStyle;
  final Color backgroundColor;

  @override
  Widget build(BuildContext context) {
    final surface = _resolveSurfaceColor(backgroundColor);
    final borderColor = _resolveBorderColor(textStyle.color);

    return Container(
      width: double.infinity,
      decoration: BoxDecoration(
        color: surface,
        borderRadius: BorderRadius.circular(16),
        border: Border.all(color: borderColor.withValues(alpha: 0.18)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          for (final req in diagram.requirements) _buildReqCard(req, borderColor),
          for (final elem in diagram.elements) _buildElemCard(elem, borderColor),
          if (diagram.relations.isNotEmpty) _buildRelations(borderColor),
          const SizedBox(height: 8),
        ],
      ),
    );
  }

  Widget _buildReqCard(ChatMermaidRequirementNode req, Color borderColor) {
    const accent = Color(0xFF1D4ED8);
    final kindLabel = _kindLabel(req.kind);
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.fromLTRB(12, 12, 12, 0),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: accent.withValues(alpha: 0.06),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: accent.withValues(alpha: 0.3)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(children: [
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
              decoration: BoxDecoration(
                color: accent.withValues(alpha: 0.14),
                borderRadius: BorderRadius.circular(4),
              ),
              child: Text(kindLabel, style: textStyle.copyWith(
                fontSize: (textStyle.fontSize ?? 13) - 4, color: accent, fontWeight: FontWeight.w700)),
            ),
            const SizedBox(width: 8),
            Expanded(child: Text(req.name, style: textStyle.copyWith(fontWeight: FontWeight.w700, fontSize: (textStyle.fontSize ?? 13)))),
          ]),
          if (req.id.isNotEmpty) Padding(padding: const EdgeInsets.only(top: 4), child: _field('ID', req.id)),
          if (req.text.isNotEmpty) Padding(padding: const EdgeInsets.only(top: 4), child: _field('Text', req.text)),
          if (req.risk != null) Padding(padding: const EdgeInsets.only(top: 4), child: _field('Risk', req.risk!)),
          if (req.verifyMethod != null) Padding(padding: const EdgeInsets.only(top: 4), child: _field('Verify', req.verifyMethod!)),
        ],
      ),
    );
  }

  Widget _buildElemCard(ChatMermaidRequirementElement elem, Color borderColor) {
    const accent = Color(0xFF166534);
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.fromLTRB(12, 12, 12, 0),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: accent.withValues(alpha: 0.06),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: accent.withValues(alpha: 0.3)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(children: [
            Container(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
              decoration: BoxDecoration(color: accent.withValues(alpha: 0.14), borderRadius: BorderRadius.circular(4)),
              child: Text('chat_mermaid_requirement_element'.tr, style: textStyle.copyWith(fontSize: (textStyle.fontSize ?? 13) - 4, color: accent, fontWeight: FontWeight.w700)),
            ),
            const SizedBox(width: 8),
            Expanded(child: Text(elem.name, style: textStyle.copyWith(fontWeight: FontWeight.w700))),
          ]),
          if (elem.elementType != null) Padding(padding: const EdgeInsets.only(top: 4), child: _field('Type', elem.elementType!)),
          if (elem.docref != null) Padding(padding: const EdgeInsets.only(top: 4), child: _field('DocRef', elem.docref!)),
        ],
      ),
    );
  }

  Widget _buildRelations(Color borderColor) {
    return Container(
      width: double.infinity,
      margin: const EdgeInsets.fromLTRB(12, 12, 12, 0),
      padding: const EdgeInsets.all(10),
      decoration: BoxDecoration(
        color: borderColor.withValues(alpha: 0.04),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: borderColor.withValues(alpha: 0.12)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('chat_mermaid_requirement_relations'.tr, style: textStyle.copyWith(fontWeight: FontWeight.w700, fontSize: (textStyle.fontSize ?? 13) - 1)),
          const SizedBox(height: 4),
          for (final rel in diagram.relations)
            Padding(
              padding: const EdgeInsets.only(top: 3),
              child: Text(
                '${rel.sourceName}  ─${rel.type}→  ${rel.targetName}',
                style: textStyle.copyWith(fontSize: (textStyle.fontSize ?? 13) - 2),
              ),
            ),
        ],
      ),
    );
  }

  Widget _field(String label, String value) {
    return Text.rich(
      TextSpan(children: [
        TextSpan(text: '$label: ', style: textStyle.copyWith(fontSize: (textStyle.fontSize ?? 13) - 2, fontWeight: FontWeight.w600, color: textStyle.color?.withValues(alpha: 0.6))),
        TextSpan(text: value, style: textStyle.copyWith(fontSize: (textStyle.fontSize ?? 13) - 2)),
      ]),
    );
  }

  String _kindLabel(ChatMermaidRequirementKind kind) {
    switch (kind) {
      case ChatMermaidRequirementKind.requirement: return 'Requirement';
      case ChatMermaidRequirementKind.functionalRequirement: return 'Functional';
      case ChatMermaidRequirementKind.interfaceRequirement: return 'Interface';
      case ChatMermaidRequirementKind.performanceRequirement: return 'Performance';
      case ChatMermaidRequirementKind.physicalRequirement: return 'Physical';
      case ChatMermaidRequirementKind.designConstraint: return 'Constraint';
    }
  }

  Color _resolveSurfaceColor(Color background) {
    final brightness = ThemeData.estimateBrightnessForColor(background);
    return brightness == Brightness.dark ? Colors.white.withValues(alpha: 0.04) : Colors.white.withValues(alpha: 0.9);
  }

  Color _resolveBorderColor(Color? textColor) => (textColor ?? const Color(0xFF2A2214)).withValues(alpha: 0.86);
}
