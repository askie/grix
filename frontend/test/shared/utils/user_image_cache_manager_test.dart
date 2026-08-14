import 'package:flutter_test/flutter_test.dart';

import 'package:grix/shared/utils/user_image_cache_manager.dart';

void main() {
  tearDown(() {
    UserImageCacheManager.setEvictOverrideForTest(null);
  });

  test('cacheKeyForImageUrl removes volatile signing parameters', () {
    final first = UserImageCacheManager.cacheKeyForImageUrl(
      ' https://cdn.example.com/avatar/u1.png?Expires=100&Signature=abc&OSSAccessKeyId=k&v=7#ignored ',
    );
    final second = UserImageCacheManager.cacheKeyForImageUrl(
      'https://cdn.example.com/avatar/u1.png?Signature=def&Expires=200&OSSAccessKeyId=k2&v=7',
    );

    expect(first, 'https://cdn.example.com/avatar/u1.png?v=7');
    expect(second, first);
  });

  test('cacheKeyForImageUrl preserves non signing version parameters', () {
    expect(
      UserImageCacheManager.cacheKeyForImageUrl(
        'https://cdn.example.com/avatar/u1.png?version=1&size=small',
      ),
      'https://cdn.example.com/avatar/u1.png?size=small&version=1',
    );
  });

  test('cacheKeyForImageUrl preserves image transform parameters', () {
    expect(
      UserImageCacheManager.cacheKeyForImageUrl(
        'https://cdn.example.com/avatar/u1.png?x-oss-process=image/resize,w_96&x-oss-signature=s1',
      ),
      'https://cdn.example.com/avatar/u1.png?x-oss-process=image%2Fresize%2Cw_96',
    );
  });

  test('cacheKeyForImageUrl removes tencent cos signing parameters', () {
    expect(
      UserImageCacheManager.cacheKeyForImageUrl(
        'https://aibot-1252145388.cos.accelerate.myqcloud.com/avatar/u1.jpg?q-sign-algorithm=sha1&q-ak=ak&q-sign-time=1;2&q-key-time=1;2&q-header-list=host&q-url-param-list=&q-signature=s&x-cos-security-token=t&size=small',
      ),
      'https://aibot-1252145388.cos.accelerate.myqcloud.com/avatar/u1.jpg?size=small',
    );
  });

  test('evictUserImages trims and deduplicates urls', () async {
    final evictedUrls = <String>[];
    UserImageCacheManager.setEvictOverrideForTest((imageUrl) async {
      evictedUrls.add(imageUrl);
    });

    await UserImageCacheManager.evictUserImages(' user_1 ', const <String>[
      '',
      '  ',
      'https://cdn.example.com/avatar/a.png',
      ' https://cdn.example.com/avatar/a.png ',
      'https://cdn.example.com/avatar/b.png',
    ]);

    expect(evictedUrls, <String>[
      'https://cdn.example.com/avatar/a.png',
      'https://cdn.example.com/avatar/b.png',
    ]);
  });

  test('evictUserImages does nothing when user id is empty', () async {
    final evictedUrls = <String>[];
    UserImageCacheManager.setEvictOverrideForTest((imageUrl) async {
      evictedUrls.add(imageUrl);
    });

    await UserImageCacheManager.evictUserImages('   ', const <String>[
      'https://cdn.example.com/avatar/a.png',
    ]);

    expect(evictedUrls, isEmpty);
  });

  test('evictUserImage delegates to single url eviction', () async {
    final evictedUrls = <String>[];
    UserImageCacheManager.setEvictOverrideForTest((imageUrl) async {
      evictedUrls.add(imageUrl);
    });

    await UserImageCacheManager.evictUserImage(
      'user_2',
      ' https://cdn.example.com/avatar/c.png ',
    );

    expect(evictedUrls, <String>['https://cdn.example.com/avatar/c.png']);
  });
}
