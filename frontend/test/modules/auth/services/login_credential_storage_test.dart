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
      const LoginCredentialState(
        account: 'cn_user@example.com',
        password: 'CnPass123',
      ),
      AppRegion.cn,
    );

    final prefs = await SharedPreferences.getInstance();
    expect(prefs.getString('login_saved_account_cn'), 'cn_user@example.com');
    expect(prefs.getString('login_saved_password_cn'), 'CnPass123');

    final loaded = await storage.load(AppRegion.cn);
    expect(loaded.account, 'cn_user@example.com');
    expect(loaded.password, 'CnPass123');
  });

  test('save and load roundtrip for global region', () async {
    await storage.save(
      const LoginCredentialState(
        account: 'global_user@example.com',
        password: 'GlobalPass123',
      ),
      AppRegion.global,
    );

    final prefs = await SharedPreferences.getInstance();
    expect(
      prefs.getString('login_saved_account_global'),
      'global_user@example.com',
    );
    expect(prefs.getString('login_saved_password_global'), 'GlobalPass123');

    final loaded = await storage.load(AppRegion.global);
    expect(loaded.account, 'global_user@example.com');
    expect(loaded.password, 'GlobalPass123');
  });

  test('cn and global credentials are stored independently', () async {
    await storage.save(
      const LoginCredentialState(account: 'cn@example.com', password: 'cnpwd'),
      AppRegion.cn,
    );
    await storage.save(
      const LoginCredentialState(
        account: 'global@example.com',
        password: 'globalpwd',
      ),
      AppRegion.global,
    );

    final cn = await storage.load(AppRegion.cn);
    final global = await storage.load(AppRegion.global);

    expect(cn.account, 'cn@example.com');
    expect(cn.password, 'cnpwd');
    expect(global.account, 'global@example.com');
    expect(global.password, 'globalpwd');
  });

  test('writing cn credentials does not affect global credentials', () async {
    await storage.save(
      const LoginCredentialState(
        account: 'global@example.com',
        password: 'globalpwd',
      ),
      AppRegion.global,
    );

    await storage.save(
      const LoginCredentialState(
        account: 'cn_new@example.com',
        password: 'cnpwd_new',
      ),
      AppRegion.cn,
    );

    final global = await storage.load(AppRegion.global);
    expect(global.account, 'global@example.com');
    expect(global.password, 'globalpwd');
  });

  test('load returns empty state when nothing saved for region', () async {
    await storage.save(
      const LoginCredentialState(account: 'cn@example.com', password: 'cnpwd'),
      AppRegion.cn,
    );

    final global = await storage.load(AppRegion.global);
    expect(global.account, '');
    expect(global.password, '');
  });

  test('save empty state clears credentials for that region only', () async {
    await storage.save(
      const LoginCredentialState(account: 'cn@example.com', password: 'cnpwd'),
      AppRegion.cn,
    );
    await storage.save(
      const LoginCredentialState(
        account: 'global@example.com',
        password: 'globalpwd',
      ),
      AppRegion.global,
    );

    await storage.save(const LoginCredentialState(), AppRegion.cn);

    final cn = await storage.load(AppRegion.cn);
    final global = await storage.load(AppRegion.global);

    expect(cn.account, '');
    expect(cn.password, '');
    expect(global.account, 'global@example.com');
    expect(global.password, 'globalpwd');
  });

  test('account is trimmed on save', () async {
    await storage.save(
      const LoginCredentialState(
        account: '  spaces@example.com  ',
        password: 'pwd',
      ),
      AppRegion.cn,
    );

    final loaded = await storage.load(AppRegion.cn);
    expect(loaded.account, 'spaces@example.com');
  });
}
