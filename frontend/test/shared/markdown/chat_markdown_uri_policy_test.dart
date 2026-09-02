import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/markdown/chat_markdown_uri_policy.dart';

void main() {
  test('allows only approved link schemes', () {
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri('https://example.com')?.scheme,
      'https',
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri('http://example.com')?.scheme,
      'http',
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri(
        'mailto:test@example.com',
      )?.scheme,
      'mailto',
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri('tel:+123456789')?.scheme,
      'tel',
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri(
        'sinaweibo://detail?mblogid=5332538276970973',
      )?.scheme,
      'sinaweibo',
    );

    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri('javascript:alert(1)'),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri('file:///tmp/a.txt'),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri('//example.com/path'),
      isNull,
    );
    expect(ChatMarkdownUriPolicy.resolveSafeLinkUri('/relative/path'), isNull);
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri(
        'sinaweibo://detail?mblogid=not-a-number',
      ),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri(
        'sinaweibo://detail?mblogid=5332538276970973&action=share',
      ),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri('sinaweibo://compose'),
      isNull,
    );
  });

  test('resolves only absolute agent file paths', () {
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath(
        '/workspace/My%20Project/README.md',
      ),
      '/workspace/My Project/README.md',
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath(r'C:\work\README.md'),
      r'C:\work\README.md',
    );

    expect(ChatMarkdownUriPolicy.resolveAgentFilePath('README.md'), isNull);
    expect(ChatMarkdownUriPolicy.resolveAgentFilePath('../README.md'), isNull);
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath('//example.com/README.md'),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath('/tmp/%00README.md'),
      isNull,
    );
  });

  test('resolves local file URIs to agent file paths', () {
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath('file:///tmp/README.md'),
      '/tmp/README.md',
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath('FILE:///tmp/README.md'),
      '/tmp/README.md',
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath(
        'file://localhost/tmp/README.md',
      ),
      '/tmp/README.md',
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath(
        'file:///workspace/My%20Project/README.md',
      ),
      '/workspace/My Project/README.md',
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath('file:///C:/work/README.md'),
      'C:/work/README.md',
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath('file:///workspace/'),
      '/workspace/',
    );

    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath(
        'file://example.com/share/README.md',
      ),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath(
        'file://user@localhost/tmp/README.md',
      ),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath('file:///tmp/%00README.md'),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath('file:///tmp/a.md?x=1'),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveAgentFilePath('file:README.md'),
      isNull,
    );
    expect(ChatMarkdownUriPolicy.resolveAgentFilePath('file://'), '/');
  });

  test('allows only approved image schemes', () {
    expect(
      ChatMarkdownUriPolicy.resolveSafeImageUri(
        'https://example.com/a.png',
      )?.scheme,
      'https',
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeImageUri(
        'http://example.com/a.png',
      )?.scheme,
      'http',
    );

    expect(
      ChatMarkdownUriPolicy.resolveSafeImageUri('data:image/png;base64,AAAA'),
      isNull,
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeImageUri('file:///tmp/a.png'),
      isNull,
    );
    expect(ChatMarkdownUriPolicy.resolveSafeImageUri('/images/a.png'), isNull);
  });
}
