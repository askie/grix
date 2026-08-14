import 'package:flutter_test/flutter_test.dart';

import 'package:grix/data/providers/deep_link_service.dart';

void main() {
  group('DeepLinkService.parsePrefixUri', () {
    test('returns parsed uri for valid prefix', () {
      final parsed = DeepLinkService.parsePrefixUri('https://dhf.pub/u/');
      expect(parsed, isNotNull);
      expect(parsed!.scheme, 'https');
      expect(parsed.host, 'dhf.pub');
    });

    test('returns null for invalid prefix', () {
      expect(DeepLinkService.parsePrefixUri('  '), isNull);
      expect(DeepLinkService.parsePrefixUri('dhf.pub/u'), isNull);
    });
  });

  group('DeepLinkService.extractFriendQrCodeWithPrefix', () {
    final prefixUri = Uri.parse('https://dhf.pub/u/');

    test('extracts code when uri matches prefix', () {
      final code = DeepLinkService.extractFriendQrCodeWithPrefix(
        incomingUri: Uri.parse('https://dhf.pub/u/ABC123'),
        prefixUri: prefixUri,
      );
      expect(code, 'ABC123');
    });

    test('supports query string and fragment', () {
      final code = DeepLinkService.extractFriendQrCodeWithPrefix(
        incomingUri: Uri.parse('https://dhf.pub/u/ABC123?from=scan#x'),
        prefixUri: prefixUri,
      );
      expect(code, 'ABC123');
    });

    test('rejects mismatched scheme', () {
      final code = DeepLinkService.extractFriendQrCodeWithPrefix(
        incomingUri: Uri.parse('http://dhf.pub/u/ABC123'),
        prefixUri: prefixUri,
      );
      expect(code, isEmpty);
    });

    test('rejects mismatched host', () {
      final code = DeepLinkService.extractFriendQrCodeWithPrefix(
        incomingUri: Uri.parse('https://example.com/u/ABC123'),
        prefixUri: prefixUri,
      );
      expect(code, isEmpty);
    });

    test('rejects mismatched port', () {
      final code = DeepLinkService.extractFriendQrCodeWithPrefix(
        incomingUri: Uri.parse('https://dhf.pub:8443/u/ABC123'),
        prefixUri: prefixUri,
      );
      expect(code, isEmpty);
    });

    test('rejects missing code segment', () {
      final code = DeepLinkService.extractFriendQrCodeWithPrefix(
        incomingUri: Uri.parse('https://dhf.pub/u/'),
        prefixUri: prefixUri,
      );
      expect(code, isEmpty);
    });

    test('rejects extra path segment', () {
      final code = DeepLinkService.extractFriendQrCodeWithPrefix(
        incomingUri: Uri.parse('https://dhf.pub/u/ABC123/detail'),
        prefixUri: prefixUri,
      );
      expect(code, isEmpty);
    });
  });

  group('DeepLinkService.extractGroupQrCodeWithPrefix', () {
    final prefixUri = Uri.parse('https://dhf.pub/g/');

    test('extracts group code when uri matches prefix', () {
      final code = DeepLinkService.extractGroupQrCodeWithPrefix(
        incomingUri: Uri.parse('https://dhf.pub/g/GROUP001'),
        prefixUri: prefixUri,
      );
      expect(code, 'GROUP001');
    });

    test('rejects mismatched path', () {
      final code = DeepLinkService.extractGroupQrCodeWithPrefix(
        incomingUri: Uri.parse('https://dhf.pub/u/GROUP001'),
        prefixUri: prefixUri,
      );
      expect(code, isEmpty);
    });
  });
}
