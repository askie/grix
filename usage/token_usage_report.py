#!/usr/bin/env python3
"""统计本机 Claude / Codex 会话的 token 消耗，按日期出柱状图。

数据来源:
  - Claude: ~/.claude/projects/**/*.jsonl  (每行 message.usage)
  - Codex:  ~/.codex/sessions/**/*.jsonl   (event_msg / token_count 的 last_token_usage)

口径:
  - input  : 非缓存输入 token(Claude 的 input_tokens;Codex 的 input_tokens - cached_input_tokens)
  - cache  : 缓存 token(Claude 的 cache_creation + cache_read;Codex 的 cached_input_tokens)
  - output : 输出 token
  - 日期按本机时区归日。

用法:
    python token_usage_report.py [YYYY-MM]    # 默认统计上个月

输出:
    - 终端打印每日明细表 + 合计
    - token_usage_YYYY-MM.png(3 行「输入/缓存/输出」 x 2 列「Claude/Codex」柱状图)

依赖: matplotlib(建议 python3 -m venv .venv && .venv/bin/pip install matplotlib)
"""
import json
import sys
from collections import defaultdict
from datetime import datetime, date
from pathlib import Path

CLAUDE_DIR = Path.home() / ".claude" / "projects"
CODEX_DIR = Path.home() / ".codex" / "sessions"


def month_range(year: int, month: int):
    start = date(year, month, 1)
    if month == 12:
        end = date(year + 1, 1, 1)
    else:
        end = date(year, month + 1, 1)
    return start, end


def local_date(ts: str):
    try:
        dt = datetime.fromisoformat(ts.replace("Z", "+00:00"))
    except (ValueError, AttributeError):
        return None
    return dt.astimezone().date()


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
                    if not usage:
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
                    if not usage:
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


def fmt(n):
    if n >= 1_000_000:
        return f"{n/1_000_000:.2f}M"
    if n >= 1_000:
        return f"{n/1_000:.1f}K"
    return str(n)


def main():
    # 默认统计上个月,也可用 `YYYY-MM` 参数指定
    if len(sys.argv) > 1:
        year, month = map(int, sys.argv[1].split("-"))
    else:
        today = date.today()
        year, month = (today.year - 1, 12) if today.month == 1 else (today.year, today.month - 1)
    start, end = month_range(year, month)
    print(f"统计区间: {start} ~ {end} (本机时区)")

    claude, n_cf = scan_claude(start, end)
    codex, n_xf = scan_codex(start, end)
    print(f"扫描文件: claude {n_cf} 个, codex {n_xf} 个")

    days = sorted(set(claude) | set(codex))
    if not days:
        print("该月没有任何会话记录。")
        return

    # 打印明细表
    header = f"{'日期':<12}{'Claude 输入':>12}{'Claude 缓存':>12}{'Claude 输出':>12}" \
             f"{'Codex 输入':>12}{'Codex 缓存':>12}{'Codex 输出':>12}"
    print(header)
    print("-" * len(header))
    totals = {"claude": new_bucket(), "codex": new_bucket()}
    for d in days:
        c, x = claude.get(d, new_bucket()), codex.get(d, new_bucket())
        for k in ("input", "cache", "output"):
            totals["claude"][k] += c[k]
            totals["codex"][k] += x[k]
        print(f"{d.isoformat():<12}{fmt(c['input']):>12}{fmt(c['cache']):>12}{fmt(c['output']):>12}"
              f"{fmt(x['input']):>12}{fmt(x['cache']):>12}{fmt(x['output']):>12}")
    print("-" * len(header))
    tc, tx = totals["claude"], totals["codex"]
    print(f"{'合计':<12}{fmt(tc['input']):>12}{fmt(tc['cache']):>12}{fmt(tc['output']):>12}"
          f"{fmt(tx['input']):>12}{fmt(tx['cache']):>12}{fmt(tx['output']):>12}")

    # 画图:上下两个子图,均为 输入/缓存/输出 堆叠柱状图
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

    # 3 行(输入/缓存/输出) x 2 列(Claude/Codex),避免缓存量级压扁输入输出
    fig, axes = plt.subplots(3, 2, figsize=(max(14, len(days) * 0.55), 11), sharex=True)
    colors = {"input": "#4C9AFF", "cache": "#F4B400", "output": "#34A853"}
    labels = {"input": L("输入", "Input"), "cache": L("缓存", "Cache"), "output": L("输出", "Output")}
    x = range(len(days))
    metrics = ("input", "cache", "output")
    for col, (tool, stats) in enumerate((("Claude", claude), ("Codex", codex))):
        for row, key in enumerate(metrics):
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
                t = totals[tool.lower()]
                ax.set_title(f"{tool}  ({L('输入', 'in')} {fmt(t['input'])} / "
                             f"{L('缓存', 'cache')} {fmt(t['cache'])} / "
                             f"{L('输出', 'out')} {fmt(t['output'])})")
            ax.set_ylabel(f"{labels[key]} tokens")
            ax.grid(axis="y", alpha=0.3)
    for col in range(2):
        axes[2][col].set_xticks(list(x))
        axes[2][col].set_xticklabels([d.strftime("%m-%d") for d in days], rotation=45, ha="right")
    fig.suptitle(L(f"{year}-{month:02d} 本机 Claude / Codex 每日 token 消耗",
                   f"Daily token usage, {year}-{month:02d}"))
    fig.tight_layout()
    out = Path(__file__).with_name(f"token_usage_{year}-{month:02d}.png")
    fig.savefig(out, dpi=150)
    print(f"图表已保存: {out}")


if __name__ == "__main__":
    main()
