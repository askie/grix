import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/profile/instance_profile.dart';

void main() {
  tearDown(InstanceProfile.resetForTest);

  group('InstanceProfile.tryNormalize', () {
    test('接受合法名并统一小写（Windows 文件系统不区分大小写）', () {
      expect(InstanceProfile.tryNormalize('Work'), 'work');
      expect(InstanceProfile.tryNormalize('profile-2'), 'profile-2');
      expect(InstanceProfile.tryNormalize('  a_B-9  '), 'a_b-9');
    });

    test('拒绝非法名', () {
      expect(InstanceProfile.tryNormalize(null), isNull);
      expect(InstanceProfile.tryNormalize(''), isNull);
      expect(InstanceProfile.tryNormalize('   '), isNull);
      expect(InstanceProfile.tryNormalize('有中文'), isNull);
      expect(InstanceProfile.tryNormalize('a b'), isNull);
      expect(InstanceProfile.tryNormalize('a/b'), isNull);
      expect(InstanceProfile.tryNormalize('..'), isNull);
      expect(InstanceProfile.tryNormalize('a' * 33), isNull);
    });
  });

  group('InstanceProfile.initialize', () {
    test('空值落到 default', () {
      expect(InstanceProfile.initialize(null), isTrue);
      expect(InstanceProfile.current.name, InstanceProfile.defaultName);
      expect(InstanceProfile.current.isDefault, isTrue);

      expect(InstanceProfile.initialize('  '), isTrue);
      expect(InstanceProfile.current.isDefault, isTrue);
    });

    test('合法名固化为当前 profile', () {
      expect(InstanceProfile.initialize('Profile-2'), isTrue);
      expect(InstanceProfile.current.name, 'profile-2');
      expect(InstanceProfile.current.isDefault, isFalse);
    });

    test('非法名返回 false 且不改变当前 profile', () {
      expect(InstanceProfile.initialize('bad name!'), isFalse);
      expect(InstanceProfile.current.isDefault, isTrue);
    });
  });
}
