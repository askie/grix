-- 对账引擎需要记录每轮观测到的厂商原始余额快照，才能算出"这段窗口期间厂商真实花了多少"。
ALTER TABLE gateway_reconciliation_reports ADD COLUMN IF NOT EXISTS vendor_balance_snapshot NUMERIC(24, 12);
