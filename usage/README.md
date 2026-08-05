# usage — 本机 AI 编程工具 token 消耗统计

统计本机 Claude Code、Codex、Kimi Code、Pi 全部会话的 token 消耗，按日期聚合出明细表和柱状图，并给出全部工具的月度综合数据。

## 数据来源与口径

- **Claude**: `~/.claude/projects/**/*.jsonl`，取每行 `message.usage`（按 message id 去重）
- **Codex**: `~/.codex/sessions/**/*.jsonl`，取 `token_count` 事件的 `last_token_usage`（每次调用的增量）
- **Kimi**: `~/.kimi-code/sessions/**/wire.jsonl`，取 `step.end` 事件的 `usage`（按 event uuid 去重，时间为行级 epoch 毫秒）
- **Pi**: `~/.pi/agent/sessions/**/*.jsonl`，取 assistant 消息的 `message.usage`（按行 id 去重）

| 指标 | Claude | Codex | Kimi | Pi |
|---|---|---|---|---|
| 输入 | `input_tokens` | `input_tokens - cached_input_tokens` | `inputOther` | `input` |
| 缓存 | `cache_creation + cache_read` | `cached_input_tokens` | `inputCacheRead + inputCacheCreation` | `cacheRead + cacheWrite` |
| 输出 | `output_tokens` | `output_tokens` | `output` | `output` |

日期按本机时区归日。新增工具时在 `token_usage_report.py` 的 `SCANNERS` 里注册扫描函数即可。

## 环境准备

```bash
python3 -m venv .venv
.venv/bin/pip install matplotlib
```

## 脚本

### token_usage_report.py — 明细表 + 月度综合 + 汇总图

```bash
.venv/bin/python token_usage_report.py            # 默认统计上个月
.venv/bin/python token_usage_report.py 2026-07    # 指定月份
```

输出：终端打印每个工具的每日明细、小计，以及全部工具的「月度综合」合计；
同时生成 `token_usage_YYYY-MM.png`（3 行「输入/缓存/输出」× 5 列「4 工具 + 综合」分面柱状图）。

### token_usage_daily.py — 每工具一张每日对比图

```bash
.venv/bin/python token_usage_daily.py             # 默认本月
.venv/bin/python token_usage_daily.py 2026-07     # 指定月份
```

输出：`token_usage_{claude,codex,kimi,pi}_YYYY-MM.png` 各一张，外加 `token_usage_all_YYYY-MM.png`（综合，全部工具逐日合计）。
每天输入/缓存/输出三根并列柱子；因缓存比输入/输出大几个数量级，Y 轴用对数刻度，
数值标签超过 1 亿用「亿」、否则用「万」。

## 注意

- 进行中的会话日志可能尚未完全落盘，当天的数据次日再跑一次才完整。
- 两个脚本需放在同一目录（`token_usage_daily.py` 导入 `token_usage_report` 的扫描函数）。
