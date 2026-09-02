import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_ast.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';
import 'package:grix/shared/markdown/chat_markdown_render_cache_codec.dart';
import 'package:grix/shared/markdown/chat_markdown_semantics.dart';

void main() {
  test('encode/decode keeps markdown render state structure', () {
    const document = ChatMarkdownDocument(
      children: <ChatMarkdownNode>[
        ChatMarkdownNode(
          type: ChatMarkdownNodeType.heading,
          attrs: <String, Object?>{'level': 2},
          children: <ChatMarkdownNode>[
            ChatMarkdownNode(
              type: ChatMarkdownNodeType.text,
              attrs: <String, Object?>{'text': 'Title'},
            ),
          ],
        ),
        ChatMarkdownNode(
          type: ChatMarkdownNodeType.list,
          attrs: <String, Object?>{'ordered': false},
          children: <ChatMarkdownNode>[
            ChatMarkdownNode(
              type: ChatMarkdownNodeType.listItem,
              children: <ChatMarkdownNode>[
                ChatMarkdownNode(
                  type: ChatMarkdownNodeType.text,
                  attrs: <String, Object?>{'text': 'item'},
                ),
              ],
            ),
          ],
        ),
      ],
    );
    const semantics = ChatMarkdownSemanticSummary(
      features: <ChatMarkdownFeature>{
        ChatMarkdownFeature.heading,
        ChatMarkdownFeature.list,
      },
    );
    const result = ChatMarkdownPipelineResult(
      originalText: '## Title\n- item',
      normalizedText: '## Title\n- item',
      shouldUseMarkdown: true,
      document: document,
      semantics: semantics,
    );

    final payload = ChatMarkdownRenderCacheCodec.encode(result);
    final decoded = ChatMarkdownRenderCacheCodec.decode(payload);

    expect(decoded, isNotNull);
    expect(decoded!.normalizedText, result.normalizedText);
    expect(decoded.shouldUseMarkdown, isTrue);
    expect(decoded.document, isNotNull);
    expect(decoded.document!.children.length, 2);
    expect(decoded.document!.children.first.type, ChatMarkdownNodeType.heading);
    expect(decoded.semantics, isNotNull);
    expect(decoded.semantics!.hasFeature(ChatMarkdownFeature.heading), isTrue);
    expect(decoded.semantics!.hasFeature(ChatMarkdownFeature.list), isTrue);
  });

  test('decode returns null for invalid payload', () {
    expect(ChatMarkdownRenderCacheCodec.decode('invalid_json'), isNull);
    expect(ChatMarkdownRenderCacheCodec.decode('{}'), isNull);
  });
}
