import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_dialect.dart';
import 'package:grix/shared/markdown/chat_markdown_normalizer.dart';
import 'package:grix/shared/markdown/chat_markdown_pipeline.dart';

void main() {
  test('probe markdown rendering for oversized text case', () {
    const raw = '''可以。让我先看看Xcode相关数据的具体情况： 找到了主要的Xcode数据：
| 目录 |大小 ||-----|-----||
CoreSimulator（模拟器） | 2.2GB||
DerivedData（编译缓存） | 1.9GB||
iOSDeviceSupport（设备支持） | 5.4GB||总计| ~9.5GB|---
两种方案： ****方案A: 直接清理（推荐）
-DerivedData可以直接删除，Xcode会自动重建，释放1.9GB
-不影响项目，只是下次编译会慢一点
方案B: 移动到外接硬盘
-把三个目录都移到/mnt/external/XcodeData/
-创建符号链接指向新位置
-风险： 外接硬盘必须一直插着，否则Xcode会出问题
''';

    final pipeline = ChatMarkdownPipeline(
      normalizer: const ChatMarkdownNormalizer(),
      parser: ChatMarkdownDialect.buildParserAdapter(),
    );
    final result = pipeline.prepareFinalRender(raw);

    expect(result.normalizedText, isNotEmpty);
    expect(result.document, isNotNull);
    expect(
      result.shouldUseMarkdown,
      result.semantics?.requiresRichRendering ?? false,
    );
  });
}
