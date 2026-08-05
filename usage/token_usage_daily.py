#!/usr/bin/env python3
"""按工具各出一张每日 token 消耗柱状图(Claude / Codex 各一张 PNG)。

每天 输入/缓存/输出 三根并列柱子;因缓存比输入/输出大几个数量级,Y 轴用对数刻度,
数值标签超过 1 亿用「亿」、否则用「万」。数据扫描逻辑在 token_usage_report.py 中,
本脚本与其放在同一目录下直接导入使用。

用法:
    python token_usage_daily.py [YYYY-MM]     # 默认本月

输出:
    token_usage_claude_YYYY-MM.png
    token_usage_codex_YYYY-MM.png
"""
import sys
from datetime import date
from pathlib import Path

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt
from matplotlib import font_manager

import token_usage_report as m


def fmt(n):
    if n >= 1e8:
        return f"{n / 1e8:.2f}亿"
    if n >= 1e4:
        return f"{n / 1e4:.1f}万"
    return str(n)


def main():
    # 默认统计本月,也可用 `YYYY-MM` 参数指定
    month = sys.argv[1] if len(sys.argv) > 1 else date.today().strftime("%Y-%m")
    year, mon = map(int, month.split("-"))
    start, end = m.month_range(year, mon)
    data = {"Claude": m.scan_claude(start, end)[0], "Codex": m.scan_codex(start, end)[0]}

    zh = None
    for name in ("PingFang SC", "Hiragino Sans GB", "Heiti SC", "Arial Unicode MS", "Noto Sans CJK SC"):
        if any(f.name == name for f in font_manager.fontManager.ttflist):
            zh = name
            break
    if zh:
        plt.rcParams["font.family"] = zh
        plt.rcParams["axes.unicode_minus"] = False

    days = sorted(set().union(*data.values()))
    colors = {"input": "#4C9AFF", "cache": "#F4B400", "output": "#34A853"}
    labels = {"input": "输入", "cache": "缓存", "output": "输出"}

    for tool, stats in data.items():
        fig, ax = plt.subplots(figsize=(max(10, len(days) * 1.2), 6))
        width = 0.27
        xs = range(len(days))
        for i, key in enumerate(("input", "cache", "output")):
            vals = [stats.get(d, m.new_bucket())[key] for d in days]
            pos = [x + (i - 1) * width for x in xs]
            ax.bar(pos, [v if v > 0 else 1 for v in vals], width=width,
                   color=colors[key], label=labels[key])
            for p, v in zip(pos, vals):
                if v > 0:
                    ax.text(p, v * 1.15, fmt(v), ha="center", va="bottom",
                            fontsize=8, rotation=90)
        ax.set_yscale("log")
        vmax = max((stats.get(d, m.new_bucket())[k] for d in days for k in colors), default=1)
        ax.set_ylim(1e2, vmax * 100)
        ax.set_xticks(list(xs))
        ax.set_xticklabels([d.strftime("%m-%d") for d in days], rotation=45, ha="right")
        totals = {k: sum(stats.get(d, m.new_bucket())[k] for d in days) for k in colors}
        ax.set_title(f"{month} {tool} 每日 token 消耗  "
                     f"(输入 {fmt(totals['input'])} / 缓存 {fmt(totals['cache'])} / 输出 {fmt(totals['output'])})")
        ax.set_ylabel("tokens (对数刻度)")
        ax.legend()
        ax.grid(axis="y", alpha=0.3)
        fig.tight_layout()
        out = Path(__file__).with_name(f"token_usage_{tool.lower()}_{month}.png")
        fig.savefig(out, dpi=150)
        print(f"{tool}: 输入 {fmt(totals['input'])} / 缓存 {fmt(totals['cache'])} / 输出 {fmt(totals['output'])}")
        print(f"图表已保存: {out}")


if __name__ == "__main__":
    main()
