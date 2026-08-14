-- Seed auth feature gates: register, Google login, Apple login.
-- auth_register defaults to enabled; social login gates default to disabled
-- and must be explicitly enabled by an admin.
INSERT INTO feature_gates (key, display_name, status)
VALUES
    ('auth_register',      '允许注册',          'enabled'),
    ('auth_google_login',  '允许 Google 登录',  'disabled'),
    ('auth_apple_login',   '允许 Apple 登录',   'disabled')
ON CONFLICT (key) DO NOTHING;
