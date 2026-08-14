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
      ChatMarkdownUriPolicy.resolveSafeLinkUri('mailto:test@example.com')
          ?.scheme,
      'mailto',
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri('tel:+123456789')?.scheme,
      'tel',
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
    expect(
      ChatMarkdownUriPolicy.resolveSafeLinkUri('/relative/path'),
      isNull,
    );
  });

  test('allows only approved image schemes', () {
    expect(
      ChatMarkdownUriPolicy.resolveSafeImageUri('https://example.com/a.png')
          ?.scheme,
      'https',
    );
    expect(
      ChatMarkdownUriPolicy.resolveSafeImageUri('http://example.com/a.png')
          ?.scheme,
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
    expect(
      ChatMarkdownUriPolicy.resolveSafeImageUri('/images/a.png'),
      isNull,
    );
  });
}
