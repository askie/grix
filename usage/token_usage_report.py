#!/usr/bin/env python3
"""统计本机 Claude / Codex / Kimi / Pi 会话的 token 消耗,按日期聚合出明细表和柱状图。

数据来源:
  - Claude: ~/.claude/projects/**/*.jsonl        (每行 message.usage,按 message.id 去重)
  - Codex:  ~/.codex/sessions/**/*.jsonl         (token_count 事件的 last_token_usage 增量)
  - Kimi:   ~/.kimi-code/sessions/**/wire.jsonl  (step.end 事件的 usage,按 event.uuid 去重)
  - Pi:     ~/.pi/agent/sessions/**/*.jsonl      (assistant 消息的 message.usage,按行 id 去重)

口径:
  - input  : 非缓存输入 token
  - cache  : 缓存 token(读 + 写/创建)
  - output : 输出 token
  - 日期按本机时区归日。

用法:
    python token_usage_report.py [YYYY-MM]    # 默认统计上个月

输出:
    - 终端打印各工具每日明细 + 每日综合(全部工具合计) + 月度合计
    - token_usage_YYYY-MM.png(3 行「输入/缓存/输出」 x 5 列「4 工具 + 综合」柱状图)

依赖: matplotlib(建议 python3 -m venv .venv && .venv/bin/pip install matplotlib)
"""
import json
import sys
from collections import defaultdict
from datetime import datetime, date
from pathlib import Path

CLAUDE_DIR = Path.home() / ".claude" / "projects"
CODEX_DIR = Path.home() / ".codex" / "sessions"
KIMI_DIR = Path.home() / ".kimi-code" / "sessions"
PI_DIR = Path.home() / ".pi" / "agent" / "sessions"


def month_range(year: int, month: int):
    start = date(year, month, 1)
    if month == 12:
        end = date(year + 1, 1, 1)
    else:
        end = date(year, month + 1, 1)
    return start, end


def local_date(ts: str):
    """ISO 时间戳 -> 本机日期"""
    try:
        dt = datetime.fromisoformat(ts.replace("Z", "+00:00"))
    except (ValueError, AttributeError):
        return None
    return dt.astimezone().date()


def local_date_ms(ms):
    """epoch 毫秒 -> 本机日期"""
    try:
        return datetime.fromtimestamp(ms / 1000).astimezone().date()
    except (OSError, OverflowError, TypeError):
        return None


def new_bucket():
    return {"input": 0, "cache": 0, "output": 0}


def scan_claude(start, end):
    stats = defaultdict(new_bucket)
    seen = set()  # message.id 去重,同一消息可能因续写出现多行
    files = list(CLAUDE_DIR.rglob("*.jsonl"))
    for fp in files:
        try:
            with open(fp, "r", encoding="utf-8", errors="replace") as f:
                for line in f:
                    if '"usage"' not in line:
                        continue
                    try:
                        rec = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    msg = rec.get("message") or {}
                    usage = msg.get("usage")
                    if not isinstance(usage, dict):
                        continue
                    mid = msg.get("id")
                    if mid:
                        if mid in seen:
                            continue
                        seen.add(mid)
                    d = local_date(rec.get("timestamp", ""))
                    if d is None or not (start <= d < end):
                        continue
                    b = stats[d]
                    b["input"] += usage.get("input_tokens", 0)
                    b["cache"] += usage.get("cache_creation_input_tokens", 0) \
                                  + usage.get("cache_read_input_tokens", 0)
                    b["output"] += usage.get("output_tokens", 0)
        except OSError:
            continue
    return stats, len(files)


def scan_codex(start, end):
    stats = defaultdict(new_bucket)
    files = list(CODEX_DIR.rglob("*.jsonl"))
    for fp in files:
        try:
            with open(fp, "r", encoding="utf-8", errors="replace") as f:
                for line in f:
                    if '"token_count"' not in line:
                        continue
                    try:
                        rec = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    payload = rec.get("payload") or {}
                    if payload.get("type") != "token_count":
                        continue
                    info = payload.get("info") or {}
                    usage = info.get("last_token_usage")  # 每次调用的增量
                    if not isinstance(usage, dict):
                        continue
                    d = local_date(rec.get("timestamp", ""))
                    if d is None or not (start <= d < end):
                        continue
                    inp = usage.get("input_tokens", 0)
                    cached = usage.get("cached_input_tokens", 0)
                    b = stats[d]
                    b["input"] += max(inp - cached, 0)
                    b["cache"] += cached
                    b["output"] += usage.get("output_tokens", 0)
        except OSError:
            continue
    return stats, len(files)


def scan_kimi(start, end):
    stats = defaultdict(new_bucket)
    seen = set()  # event.uuid 去重
    files = list(KIMI_DIR.rglob("wire.jsonl"))
    for fp in files:
        try:
            with open(fp, "r", encoding="utf-8", errors="replace") as f:
                for line in f:
                    if '"step.end"' not in line:
                        continue
                    try:
                        rec = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    ev = rec.get("event") or {}
                    if ev.get("type") != "step.end":
                        continue
                    usage = ev.get("usage")
                    if not isinstance(usage, dict):
                        continue
                    uid = ev.get("uuid")
                    if uid:
                        if uid in seen:
                            continue
                        seen.add(uid)
                    d = local_date_ms(rec.get("time"))
                    if d is None or not (start <= d < end):
                        continue
                    b = stats[d]
                    b["input"] += usage.get("inputOther", 0)
                    b["cache"] += usage.get("inputCacheRead", 0) \
                                  + usage.get("inputCacheCreation", 0)
                    b["output"] += usage.get("output", 0)
        except OSError:
            continue
    return stats, len(files)


def scan_pi(start, end):
    stats = defaultdict(new_bucket)
    seen = set()  # 行 id 去重
    files = list(PI_DIR.rglob("*.jsonl"))
    for fp in files:
        try:
            with open(fp, "r", encoding="utf-8", errors="replace") as f:
                for line in f:
                    if '"usage"' not in line:
                        continue
                    try:
                        rec = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    if rec.get("type") != "message":
                        continue
                    msg = rec.get("message") or {}
                    if msg.get("role") != "assistant":
                        continue
                    usage = msg.get("usage")
                    if not isinstance(usage, dict):
                        continue
                    mid = rec.get("id")
                    if mid:
                        if mid in seen:
                            continue
                        seen.add(mid)
                    d = local_date(rec.get("timestamp", ""))
                    if d is None or not (start <= d < end):
                        continue
                    b = stats[d]
                    b["input"] += usage.get("input", 0)
                    b["cache"] += usage.get("cacheRead", 0) + usage.get("cacheWrite", 0)
                    b["output"] += usage.get("output", 0)
        except OSError:
            continue
    return stats, len(files)


# 工具名 -> 扫描函数,新增工具在这里注册即可
SCANNERS = [
    ("Claude", scan_claude),
    ("Codex", scan_codex),
    ("Kimi", scan_kimi),
    ("Pi", scan_pi),
]
METRICS = ("input", "cache", "output")


def combine(stats_list):
    """多个工具的按日统计 -> 综合(逐日逐指标求和)"""
    out = defaultdict(new_bucket)
    for stats in stats_list:
        for d, b in stats.items():
            o = out[d]
            for k in METRICS:
                o[k] += b[k]
    return out


def fmt(n):
    if n >= 1e8:
        return f"{n / 1e8:.2f}亿"
    if n >= 1e4:
        return f"{n / 1e4:.1f}万"
    return str(n)


def main():
    # 默认统计上个月,也可用 `YYYY-MM` 参数指定
    if len(sys.argv) > 1:
        try:
            year, month = map(int, sys.argv[1].split("-"))
            month_range(year, month)
        except ValueError:
            print(f"月份参数格式不对: {sys.argv[1]!r},应为 YYYY-MM,如 2026-07")
            sys.exit(2)
    else:
        today = date.today()
        year, month = (today.year - 1, 12) if today.month == 1 else (today.year, today.month - 1)
    start, end = month_range(year, month)
    print(f"统计区间: {start} ~ {end} (本机时区)")

    data = {}
    for name, fn in SCANNERS:
        stats, n = fn(start, end)
        data[name] = stats
        print(f"扫描文件: {name} {n} 个")
    data["综合"] = combine(list(data.values()))

    days = sorted(set().union(*data.values()))
    if not days:
        print("该月没有任何会话记录。")
        return

    # 每个工具一段明细表
    grand = {}
    for name in data:
        stats = data[name]
        tot = new_bucket()
        print(f"\n[{name}]")
        print(f"{'日期':<12}{'输入':>10}{'缓存':>10}{'输出':>10}")
        for d in days:
            b = stats.get(d, new_bucket())
            for k in METRICS:
                tot[k] += b[k]
            if any(b.values()):
                print(f"{d.isoformat():<12}{fmt(b['input']):>10}{fmt(b['cache']):>10}{fmt(b['output']):>10}")
        print(f"{'小计':<12}{fmt(tot['input']):>10}{fmt(tot['cache']):>10}{fmt(tot['output']):>10}")
        grand[name] = tot

    print("\n===== 月度综合 =====")
    for name, tot in grand.items():
        print(f"{name:<8} 输入 {fmt(tot['input']):>10}  缓存 {fmt(tot['cache']):>10}  输出 {fmt(tot['output']):>10}")

    # 画图:3 行(输入/缓存/输出) x 5 列(各工具 + 综合)
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    from matplotlib import font_manager

    # 尝试找一个中文字体,找不到就用英文标签
    zh_font = None
    for name in ("PingFang SC", "Hiragino Sans GB", "Heiti SC", "Arial Unicode MS", "Noto Sans CJK SC"):
        if any(f.name == name for f in font_manager.fontManager.ttflist):
            zh_font = name
            break
    if zh_font:
        plt.rcParams["font.family"] = zh_font
        plt.rcParams["axes.unicode_minus"] = False
    L = (lambda zh, en: zh) if zh_font else (lambda zh, en: en)

    panels = list(data.items())  # [(工具名, stats), ..., (综合, stats)]
    ncols = len(panels)
    fig, axes = plt.subplots(3, ncols, figsize=(max(16, ncols * len(days) * 0.45), 11), sharex=True)
    colors = {"input": "#4C9AFF", "cache": "#F4B400", "output": "#34A853"}
    labels = {"input": L("输入", "Input"), "cache": L("缓存", "Cache"), "output": L("输出", "Output")}
    x = range(len(days))
    for col, (tool, stats) in enumerate(panels):
        for row, key in enumerate(METRICS):
            ax = axes[row][col]
            vals = [stats.get(d, new_bucket())[key] for d in days]
            ax.bar(x, vals, color=colors[key], width=0.65)
            vmax = max(vals) if vals else 0
            for xi, v in zip(x, vals):
                if v and (vmax == 0 or v >= vmax * 0.02):
                    ax.text(xi, v, fmt(v), ha="center", va="bottom", fontsize=7, rotation=90)
            if vmax:
                ax.set_ylim(0, vmax * 1.25)
            if row == 0:
                ax.set_title(tool)
            if col == 0:
                ax.set_ylabel(f"{labels[key]} tokens")
            ax.grid(axis="y", alpha=0.3)
    for col in range(ncols):
        axes[2][col].set_xticks(list(x))
        axes[2][col].set_xticklabels([d.strftime("%m-%d") for d in days], rotation=45, ha="right")
    fig.suptitle(L(f"{year}-{month:02d} 本机 AI 工具每日 token 消耗",
                   f"Daily token usage, {year}-{month:02d}"))
    fig.tight_layout()
    out = Path(__file__).with_name(f"token_usage_{year}-{month:02d}.png")
    fig.savefig(out, dpi=150)
    print(f"图表已保存: {out}")


if __name__ == "__main__":
    main()
