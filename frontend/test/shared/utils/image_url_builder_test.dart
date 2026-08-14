import 'package:flutter_test/flutter_test.dart';

import 'package:grix/shared/utils/image_url_builder.dart';

void main() {
  group('appendVersionQueryParameter', () {
    test('returns empty when source url is empty', () {
      expect(appendVersionQueryParameter('', 10), '');
      expect(appendVersionQueryParameter('   ', 10), '');
    });

    test('returns original url when version is not positive', () {
      expect(
        appendVersionQueryParameter('https://cdn.example.com/avatar.jpg', 0),
        'https://cdn.example.com/avatar.jpg',
      );
      expect(
        appendVersionQueryParameter('https://cdn.example.com/avatar.jpg', -1),
        'https://cdn.example.com/avatar.jpg',
      );
    });

    test('appends version query when url has no query', () {
      expect(
        appendVersionQueryParameter('https://cdn.example.com/avatar.jpg', 123),
        'https://cdn.example.com/avatar.jpg?v=123',
      );
    });

    test('appends version query when url already has query', () {
      expect(
        appendVersionQueryParameter(
          'https://cdn.example.com/avatar.jpg?size=small',
          123,
        ),
        'https://cdn.example.com/avatar.jpg?size=small&v=123',
      );
    });

    test('replaces existing version query with latest value', () {
      expect(
        appendVersionQueryParameter(
          'https://cdn.example.com/avatar.jpg?size=small&v=1',
          456,
        ),
        'https://cdn.example.com/avatar.jpg?size=small&v=456',
      );
    });

    test('supports custom query key', () {
      expect(
        appendVersionQueryParameter(
          'https://cdn.example.com/avatar.jpg?size=small',
          7,
          key: 'rev',
        ),
        'https://cdn.example.com/avatar.jpg?size=small&rev=7',
      );
    });
  });
}
