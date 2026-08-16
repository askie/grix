import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/auth/services/login_credential_storage.dart';
import 'package:grix/shared/utils/app_region_config.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  late LoginCredentialStorage storage;

  setUp(() {
    SharedPreferences.setMockInitialValues(<String, Object>{});
    storage = LoginCredentialStorage();
  });

  test('save and load roundtrip for cn region', () async {
    await storage.save(
      const LoginCredentialState(account: 'cn_user@example.com'),
      AppRegion.cn,
    );

    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getString('login_saved_account_cn'), 'cn_user@example.com');

    final loaded = await storage.load(AppRegion.cn);
    expect(loaded.account, 'cn_user@example.com');
  });

  test('save and load roundtrip for global region', () async {
    await storage.save(
      const LoginCredentialState(account: 'global_user@example.com'),
      AppRegion.global,
    );

    final prefs = await SharedPreferences.getInstance();
    expect(
      prefs.getString('login_saved_account_global'),
      'global_user@example.com',
    );

    final loaded = await storage.load(AppRegion.global);
    expect(loaded.account, 'global_user@example.com');
  });

  test('password is never persisted', () async {
    await storage.save(
      const LoginCredentialState(account: 'cn_user@example.com'),
      AppRegion.cn,
    );

    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getKeys().where((k) => k.contains('password')), isEmpty);
  });

  test('legacy plaintext password is removed on save and load', () async {
    SharedPreferences.setMockInitialValues(<String, Object>{
      'login_saved_account_cn': 'cn_user@example.com',
      'login_saved_password_cn': 'LegacyPass123',
    });

    await storage.load(AppRegion.cn);

    var prefs = await SharedPreferences.getInstance();
    expect(prefs.getString('login_saved_password_cn'), isNull);

    SharedPreferences.setMockInitialValues(<String, Object>{
      'login_saved_password_cn': 'LegacyPass123',
    });
    await storage.save(
      const LoginCredentialState(account: 'cn_user@example.com'),
      AppRegion.cn,
    );

    prefs = await SharedPreferences.getInstance();
    expect(prefs.getString('login_saved_password_cn'), isNull);
  });

  test('cn and global accounts are stored independently', () async {
    await storage.save(
      const LoginCredentialState(account: 'cn@example.com'),
      AppRegion.cn,
    );
    await storage.save(
      const LoginCredentialState(account: 'global@example.com'),
      AppRegion.global,
    );

    final cn = await storage.load(AppRegion.cn);
    final global = await storage.load(AppRegion.global);

    expect(cn.account, 'cn@example.com');
    expect(global.account, 'global@example.com');
  });

  test('writing cn account does not affect global account', () async {
    await storage.save(
      const LoginCredentialState(account: 'global@example.com'),
      AppRegion.global,
    );

    await storage.save(
      const LoginCredentialState(account: 'cn_new@example.com'),
      AppRegion.cn,
    );

    final global = await storage.load(AppRegion.global);
    expect(global.account, 'global@example.com');
  });

  test('load returns empty state when nothing saved for region', () async {
    await storage.save(
      const LoginCredentialState(account: 'cn@example.com'),
      AppRegion.cn,
    );

    final global = await storage.load(AppRegion.global);
    expect(global.account, '');
  });

  test('save empty state clears account for that region only', () async {
    await storage.save(
      const LoginCredentialState(account: 'cn@example.com'),
      AppRegion.cn,
    );
    await storage.save(
      const LoginCredentialState(account: 'global@example.com'),
      AppRegion.global,
    );

    await storage.save(const LoginCredentialState(), AppRegion.cn);

    final cn = await storage.load(AppRegion.cn);
    final global = await storage.load(AppRegion.global);

    expect(cn.account, '');
    expect(global.account, 'global@example.com');
  });

  test('account is trimmed on save', () async {
    await storage.save(
      const LoginCredentialState(account: '  spaces@example.com  '),
      AppRegion.cn,
    );

    final loaded = await storage.load(AppRegion.cn);
    expect(loaded.account, 'spaces@example.com');
  });
}
