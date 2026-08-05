# usage — 本机 Claude / Codex token 消耗统计

统计本机 Claude Code 与 Codex 全部会话的 token 消耗，按日期聚合出柱状图。

## 数据来源与口径

- **Claude**: `~/.claude/projects/**/*.jsonl`，取每行 `message.usage`（按 message id 去重）
- **Codex**: `~/.codex/sessions/**/*.jsonl`，取 `token_count` 事件的 `last_token_usage`（每次调用的增量）

| 指标 | Claude | Codex |
|---|---|---|
| 输入 | `input_tokens` | `input_tokens - cached_input_tokens` |
| 缓存 | `cache_creation_input_tokens + cache_read_input_tokens` | `cached_input_tokens` |
| 输出 | `output_tokens` | `output_tokens` |

日期按本机时区归日。

## 环境准备

```bash
python3 -m venv .venv
.venv/bin/pip install matplotlib
```

## 脚本

### token_usage_report.py — 明细表 + 汇总图

```bash
.venv/bin/python token_usage_report.py            # 默认统计上个月
.venv/bin/python token_usage_report.py 2026-07    # 指定月份
```

输出：终端打印每日明细表 + 合计；同时生成 `token_usage_YYYY-MM.png`
（3 行「输入/缓存/输出」× 2 列「Claude/Codex」分面柱状图）。

### token_usage_daily.py — 每工具一张每日对比图

```bash
.venv/bin/python token_usage_daily.py             # 默认本月
.venv/bin/python token_usage_daily.py 2026-07     # 指定月份
```

输出：`token_usage_claude_YYYY-MM.png`、`token_usage_codex_YYYY-MM.png`。
每天输入/缓存/输出三根并列柱子；因缓存比输入/输出大几个数量级，Y 轴用对数刻度，
数值标签超过 1 亿用「亿」、否则用「万」。

## 注意

- 进行中的会话日志可能尚未完全落盘，当天的数据次日再跑一次才完整。
- 两个脚本需放在同一目录（`token_usage_daily.py` 导入 `token_usage_report` 的扫描函数）。
