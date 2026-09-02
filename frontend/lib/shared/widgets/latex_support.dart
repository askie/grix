import 'package:flutter/material.dart';
import 'package:grix/app/themes/app_theme.dart';
import 'package:flutter_math_fork/flutter_math.dart';
import 'package:markdown_widget/markdown_widget.dart';
import 'package:markdown/markdown.dart' as md;

import 'chat_markdown_latex_render_normalizer.dart';

/// Renders a block-level (display) LaTeX formula.
class LatexBlockNode extends SpanNode {
  final md.Element element;
  final Color textColor;

  LatexBlockNode(this.element, {this.textColor = Colors.black});

  @override
  InlineSpan build() {
    final tex = element.attributes['tex'] ?? element.textContent;
    final renderTex =
        ChatMarkdownLatexRenderNormalizer.normalizeForMathRenderer(tex);
    return WidgetSpan(
      child: Container(
        width: double.infinity,
        padding: const EdgeInsets.symmetric(vertical: 8),
        child: Center(
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Math.tex(
              renderTex,
              textStyle: TextStyle(fontSize: 16, color: textColor),
              mathStyle: MathStyle.display,
              onErrorFallback: (err) => SelectableText(
                '\$\$$renderTex\$\$',
                style: TextStyle(
                  fontFamily: 'monospace',
                  fontFamilyFallback: AppTheme.textFontFallbackOrNull,
                  fontSize: 14,
                  color: textColor.withValues(alpha: 0.7),
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// Renders an inline LaTeX formula.
class LatexInlineNode extends SpanNode {
  final md.Element element;
  final Color textColor;

  LatexInlineNode(this.element, {this.textColor = Colors.black});

  @override
  InlineSpan build() {
    final tex = element.attributes['tex'] ?? element.textContent;
    return WidgetSpan(
      alignment: PlaceholderAlignment.middle,
      child: Math.tex(
        tex,
        textStyle: TextStyle(fontSize: 14, color: textColor),
        mathStyle: MathStyle.text,
        onErrorFallback: (err) => Text(
          '\$$tex\$',
          style: TextStyle(
            fontFamily: 'monospace',
            fontFamilyFallback: AppTheme.textFontFallbackOrNull,
            fontSize: 13,
            color: textColor.withValues(alpha: 0.7),
          ),
        ),
      ),
    );
  }
}

/// Helper to create LaTeX block node generator.
SpanNodeGeneratorWithTag latexBlockGenerator({Color textColor = Colors.black}) {
  return SpanNodeGeneratorWithTag(
    tag: 'latex-block',
    generator: (e, config, visitor) => LatexBlockNode(e, textColor: textColor),
  );
}

/// Helper to create LaTeX inline node generator.
SpanNodeGeneratorWithTag latexInlineGenerator({
  Color textColor = Colors.black,
}) {
  return SpanNodeGeneratorWithTag(
    tag: 'latex-inline',
    generator: (e, config, visitor) => LatexInlineNode(e, textColor: textColor),
  );
}
