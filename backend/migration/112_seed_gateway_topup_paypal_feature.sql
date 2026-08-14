-- PayPal 自助充值入口：默认关闭。
-- 两区独立数据库分别配置：国内区保持 disabled，全球区在塘主后台启用。
INSERT INTO feature_gates (key, display_name, status)
VALUES ('gateway_topup_paypal', '自助充值 PayPal', 'disabled')
ON CONFLICT (key) DO NOTHING;
