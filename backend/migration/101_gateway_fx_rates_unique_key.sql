-- gateway_fx_rates 加唯一键 (from_currency, to_currency, effective_from)。
--
-- 背景：fxsync 以「数据源报价时间」为幂等键，靠 Count → Create 的 check-then-act 去重，
-- 而跨副本互斥用的 advisory lock 在取锁失败或非 Postgres 时会降级放行。缺唯一约束时，
-- 这条路径在并发下可能落下重复行。
--
-- 去重策略：同键重复行只保留 id 最小的那条。
-- 依据：全部三处读取路径（wallet.EffectiveRate、fxsync.deviationBase、fxsync.warnIfStale）
-- 都用 GORM 的 First()，它在 ORDER BY effective_from DESC 之后追加主键升序，
-- 因此同键重复行中恒定只有 id 最小的那条会被选中，其余行任何查询都读不到。
-- 已在真实 Postgres 上实测验证（三条同键不同 rate 的行，EffectiveRate 只读到 id 最小那条）。
-- 故删除其余行不改变任何可观察行为，也不会影响已入账的充值流水
-- （流水在 gateway_topup_records 里独立记录了当时用的 fx_rate_used，不回查本表）。
--
-- 最坏情况的兜底不在上面的论证，而在迁移引擎：internal/store/migrate.go 让每个迁移文件
-- 跑在独立事务里。万一 DELETE 漏清了脏数据、CREATE UNIQUE INDEX 因残留重复行失败，
-- 整个文件连同 DELETE 一起回滚，绝不会留下「数据删了、索引没建、schema_migrations 也没记」
-- 的半吊子库。这道防线比论证本身更硬。

DELETE FROM gateway_fx_rates a
USING gateway_fx_rates b
WHERE a.from_currency = b.from_currency
  AND a.to_currency = b.to_currency
  AND a.effective_from = b.effective_from
  AND a.id > b.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_gateway_fx_rates_key
  ON gateway_fx_rates (from_currency, to_currency, effective_from);

-- 084 建的 idx_gateway_fx_rates_lookup (from_currency, to_currency, effective_from DESC)
-- 与上面的唯一索引列序完全相同（Postgres 可反向扫描），至此已是纯冗余，白吃一份写放大。
DROP INDEX IF EXISTS idx_gateway_fx_rates_lookup;
