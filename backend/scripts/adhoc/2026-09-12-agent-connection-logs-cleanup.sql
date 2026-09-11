-- 存量脏数据清理：agent_connection_logs.disconnected_at 长期漏写导致的历史堆积。
--
-- 背景：连接异常结束的路径（进程被 SIGKILL / OOM-kill、优雅关停来不及走完、
-- 顶号旧连接漏回填）此前没有可靠回填 disconnected_at，两个多月里在
-- agent_connection_logs 里堆了大量 disconnected_at 仍是 NULL 的"看似在线"记录。
-- 代码修复见 fix/agent-conn-log-disconnect 分支：
--   - 服务端优雅关停已回填本节点连接（既有逻辑，已补测试）；
--   - 同一 agent 新连接顶替旧连接已回填旧记录（既有逻辑，已补测试）；
--   - 新增：ws 节点启动时对账，关闭本节点(node_id 精确匹配)遗留的未关闭记录，
--     兜底进程被强杀的场景（backend/internal/ws/agentapi/conn_security.go
--     ReconcileStaleConnectionLogsOnStartup）。
--
-- 本脚本只负责清理"代码修复上线之前"已经存在的存量脏数据，且不会被任何自动
-- 化流程执行——backend/migration/ 目录下的文件才会被 cmd/migrate 自动应用，
-- 本脚本刻意放在 backend/scripts/adhoc/ 之外，需要人工用 psql 之类的工具连
-- 到目标库手动执行，执行前请先确认已经连到了正确的库（CN / 海外）。
--
-- 清理策略：
--   按 (agent_id, owner_id) 分组——这是连接身份的真实粒度，同一 agent 被多个
--   owner（主人 + 被共享者）各自持有一条独立连接，互不影响；ws 层的连接表
--   (Manager.conns) 本身就保证同一时刻同一 (agent_id, owner_id) 只会有一条
--   活跃连接。disconnected_at 仍为 NULL 的行里，只保留每组 connected_at
--   最新的一条，其余全部标记为清理关闭——如果这个 (agent_id, owner_id) 组合
--   当前确实在线，真实连接必然就是这一组里最新的那一条，因此这条规则不会
--   误关任何真实在线的连接。
--
-- 遗留风险说明（不需要额外处理，供你知情）：
--   每组保留的"最新一条"里，有一部分其实也早就是脏数据（这个 agent 已经下线
--   很久，只是它恰好是该分组里最后一条，本脚本保守起见不做主动下线判断）。
--   这部分残留会在对应 ws 节点下一次重启时被新增的启动对账逻辑自动关闭
--   （按 node_id 精确匹配、不看 agent），不需要为它们单独再跑一次脚本。
--
-- 使用方法：
--   1) 先执行 Step 1 的 SELECT 预览，确认待清理的行数、按 node_id 的分布
--      与你在只读库上观察到的量级（约 1519 条，node_id=aibot-ws-0/aibot-ws-1）
--      基本吻合。
--   2) 确认无误后再执行 Step 2。UPDATE 包在事务里，先看返回的影响行数是否
--      与 Step 1 的预览总数一致：一致再执行 COMMIT；不一致（比如两步之间又有
--      新连接落库改变了排名）就 ROLLBACK，重新从 Step 1 开始核对。
--   3) disconnect_reason 统一标记为 'legacy_cleanup_2026_09'，与代码里两条
--      正常回填路径的 reason（'closed' / 'connection_superseded' /
--      'replaced_by_new_connection'）以及启动对账的 'startup_reconcile'
--      区分开，方便事后追溯这批数据是通过本脚本一次性清理的，而不是真实断开。

-- ============================================================
-- Step 1: 预览——只读，可以随时重复执行，不影响数据
-- ============================================================
SELECT
    node_id,
    COUNT(*) AS stale_rows_to_close
FROM (
    SELECT
        id,
        node_id,
        ROW_NUMBER() OVER (
            PARTITION BY agent_id, owner_id
            ORDER BY connected_at DESC
        ) AS rn
    FROM agent_connection_logs
    WHERE disconnected_at IS NULL
) ranked
WHERE rn > 1
GROUP BY node_id
ORDER BY node_id;

-- 想看清理后每个 (agent_id, owner_id) 分组还会剩几条 NULL(应当恰好各剩 1 条)：
-- SELECT agent_id, owner_id, COUNT(*)
-- FROM agent_connection_logs
-- WHERE disconnected_at IS NULL
-- GROUP BY agent_id, owner_id
-- HAVING COUNT(*) > 1
-- ORDER BY COUNT(*) DESC;

-- 单个 agent 最多堆积多少条(用于跟你之前观察到的 67 条对照):
-- SELECT agent_id, COUNT(*) AS open_rows
-- FROM agent_connection_logs
-- WHERE disconnected_at IS NULL
-- GROUP BY agent_id
-- ORDER BY open_rows DESC
-- LIMIT 20;

-- ============================================================
-- Step 2: 清理——包在事务里，核对影响行数后再决定 COMMIT 还是 ROLLBACK
-- ============================================================
BEGIN;

WITH ranked AS (
    SELECT
        id,
        ROW_NUMBER() OVER (
            PARTITION BY agent_id, owner_id
            ORDER BY connected_at DESC
        ) AS rn
    FROM agent_connection_logs
    WHERE disconnected_at IS NULL
)
UPDATE agent_connection_logs
SET
    disconnected_at = NOW(),
    disconnect_reason = 'legacy_cleanup_2026_09'
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

-- 核对上面 UPDATE 命令返回的 "UPDATE <n>" 行数是否与 Step 1 预览的总数一致。
-- 一致：
--   COMMIT;
-- 不一致（说明清理窗口内又有新连接落库改变了排名，属于正常情况）：
--   ROLLBACK;
-- 然后回到 Step 1 重新核对一遍再执行。

-- 本脚本执行完成、且已 COMMIT 之后，再手动执行下面这行确认没有遗漏
-- （预期返回值等于清理前"每组剩余最新一条"的分组数，即不同 (agent_id, owner_id)
-- 组合数，而不是 0——每组保留的最新一条仍然会显示 NULL，这是预期行为）：
-- SELECT COUNT(*) FROM agent_connection_logs WHERE disconnected_at IS NULL;
