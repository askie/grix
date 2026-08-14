import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/profile/widgets/widget_site_embed_sheet.dart';

void main() {
  const base =
      '<script src="https://grix.example/public/widget/widget.js" data-site-key="wk_abc" defer></script>';

  test('follow browser omits data-locale', () {
    expect(applyEmbedLocale(base, null), base);
    expect(applyEmbedLocale(base, ''), base);
    expect(applyEmbedLocale(base, '  '), base);
  });

  test('fixed locale injects data-locale before defer', () {
    expect(
      applyEmbedLocale(base, 'zh_CN'),
      '<script src="https://grix.example/public/widget/widget.js" data-site-key="wk_abc" data-locale="zh_CN" defer></script>',
    );
  });

  test('switching locale replaces previous data-locale', () {
    final withZh = applyEmbedLocale(base, 'zh_CN');
    expect(
      applyEmbedLocale(withZh, 'ja_JP'),
      '<script src="https://grix.example/public/widget/widget.js" data-site-key="wk_abc" data-locale="ja_JP" defer></script>',
    );
    expect(applyEmbedLocale(withZh, null), base);
  });
}
