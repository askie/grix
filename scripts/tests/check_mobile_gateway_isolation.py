#!/usr/bin/env python3
"""移动端「模型设置」充值隔离静态检查（M4，设计 docs/frontend/gateway_relay_mobile_design.md §3.0）。

商店合规红线：移动端模型设置页面必须做到代码结构隔离充值功能——页面不得
import 桌面充值面板、不得调用充值下单/流水查询链路、不得引用充值相关
i18n key（运行时 if 隐藏不达标，tree-shaker 不会移除未执行分支里的代码与
字符串）。

检查规则：
  1. 以移动端页面目录 frontend/lib/modules/gateway/** 为种子，沿 import 递归
     解析项目内依赖（相对 import 与 package:grix/），得到可达依赖闭包；
     闭包内任何文件 import 桌面面板 gateway_relay_panel_view.dart → 违规。
     例外：路由表 app_routes.dart 是全应用页面注册表（按设计 import 所有
     页面，含桌面面板所在模块），在闭包中作为终止节点——本身参与扫描，
     但其 import 不再展开，否则任何页面都能经路由表"可达"桌面面板；
  2. 闭包内任何文件引用充值/余额/流水相关 i18n key
     （gateway_relay_topup*/channel_*/amount_*/pay_*/balance_* 等）→ 违规；
  3. 闭包内任何文件（除共享数据层 gateway_service.dart 外）出现
     _TopupDialog/createTopup/listTopups/listLedger/getWallet 符号 → 违规。
     gateway_service.dart 是桌面/移动端共享的 Service，允许它*定义*这些方法，
     红线是移动端页面不得*调用*；
  4. 入口宿主文件（settings_view.dart、app_routes.dart）只做直接扫描、不展开
     闭包——路由表按设计要 import 全应用页面（含桌面面板所在的 system 模块），
     展开闭包会把整个桌面端算进来，不是本规则意图；
  5. 纯注释行（// 或 /// 开头）不参与符号/key 匹配——注释不进编译产物。

编译产物级字符串扫描（§3.0 第 2 条，release 构建后验证 tree-shaking 结果）
不在本脚本范围。

用法：
  python3 scripts/tests/check_mobile_gateway_isolation.py
退出码：0 = 通过；1 = 发现违规（明细打印到 stdout）。命中即应阻断构建。
"""

import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[2]
LIB = REPO_ROOT / "frontend" / "lib"

# 种子：移动端「模型设置」页面目录（闭包根）。
SEED_DIR = LIB / "modules" / "gateway"

# 入口宿主文件：只直接扫描，不展开 import 闭包（见 docstring 规则 4）。
HOST_FILES = [
    LIB / "modules" / "profile" / "settings_view.dart",
    LIB / "app" / "routes" / "app_routes.dart",
]

# 闭包中的终止节点：参与扫描但 import 不再展开（见 docstring 规则 1 例外）。
TERMINAL_FILES = {
    (LIB / "app" / "routes" / "app_routes.dart").resolve(),
}

# 共享数据层：允许定义充值方法（桌面端在用），豁免符号调用检查。
SHARED_SERVICE = LIB / "data" / "providers" / "gateway_service.dart"

# 桌面充值面板：移动端页面闭包内不得 import。
DESKTOP_PANEL = LIB / "modules" / "system" / "gateway_relay_panel_view.dart"

IMPORT_RE = re.compile(r"""^\s*import\s+['"]([^'"]+)['"]""")

# 充值/余额/流水相关 i18n key（§3.0：topup_*/channel_*/amount_* 及支付/余额类）。
FORBIDDEN_KEY_RE = re.compile(
    r"gateway_relay_(?:topup|channel_|amount_|go_pay|pay_|balance_|"
    r"invalid_amount|admin_topup|empty_topups|tab_topups|tab_ledger|empty_ledger)"
)

# 充值链路符号（在豁免文件之外出现即视为移动端调用了充值链路）。
FORBIDDEN_SYMBOL_RE = re.compile(
    r"\b(?:_TopupDialog|createTopup|listTopups|listLedger|getWallet)\b"
)


def resolve_import(importer: Path, spec: str) -> Path | None:
    """把 import 说明符解析成项目内文件；包外依赖（flutter/dio 等）返回 None。"""
    if spec.startswith("package:grix/"):
        candidate = LIB / spec[len("package:grix/"):]
    elif spec.startswith("dart:") or spec.startswith("package:"):
        return None
    else:
        candidate = (importer.parent / spec).resolve()
    try:
        candidate.relative_to(LIB)
    except ValueError:
        return None
    return candidate if candidate.is_file() else None


def collect_closure(seeds: list[Path]) -> dict[Path, list[Path]]:
    """DFS 可达闭包：{文件: 它 import 的项目内文件列表}。"""
    graph: dict[Path, list[Path]] = {}
    stack = [p.resolve() for p in seeds]
    while stack:
        path = stack.pop()
        if path in graph:
            continue
        if path in TERMINAL_FILES:
            graph[path] = []
            continue
        try:
            text = path.read_text(encoding="utf-8")
        except OSError:
            graph[path] = []
            continue
        deps = []
        for line in text.splitlines():
            m = IMPORT_RE.match(line)
            if not m:
                continue
            dep = resolve_import(path, m.group(1))
            if dep is not None:
                deps.append(dep)
                if dep not in graph:
                    stack.append(dep)
        graph[path] = deps
    return graph


def scan_file(path: Path, check_symbols: bool, violations: list[str]) -> None:
    """扫描单个文件的 i18n key 引用与（可选）充值符号；纯注释行跳过。"""
    rel = path.relative_to(REPO_ROOT)
    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        violations.append(f"{rel}: 读取失败")
        return
    for lineno, line in enumerate(lines, 1):
        if line.lstrip().startswith("//"):
            continue
        for m in FORBIDDEN_KEY_RE.finditer(line):
            violations.append(f"{rel}:{lineno}: 引用充值相关 i18n key '{m.group(0)}'")
        if check_symbols:
            for m in FORBIDDEN_SYMBOL_RE.finditer(line):
                violations.append(f"{rel}:{lineno}: 出现充值链路符号 '{m.group(0)}'")


def main() -> int:
    if not SEED_DIR.is_dir():
        print(f"FAIL: 移动端页面目录不存在: {SEED_DIR.relative_to(REPO_ROOT)}")
        return 1
    seeds = sorted(SEED_DIR.rglob("*.dart"))
    missing = [str(p.relative_to(REPO_ROOT)) for p in HOST_FILES if not p.is_file()]
    if not seeds or missing:
        print(f"FAIL: 种子文件缺失: {missing or 'modules/gateway 下无 dart 文件'}")
        return 1

    violations: list[str] = []

    # 规则 1-3：移动端页面可达依赖闭包。
    closure = collect_closure(seeds)
    for path, deps in sorted(closure.items()):
        if DESKTOP_PANEL in deps:
            violations.append(
                f"{path.relative_to(REPO_ROOT)}: import 了桌面面板 "
                f"{DESKTOP_PANEL.relative_to(REPO_ROOT)}"
            )
        scan_file(path, check_symbols=path != SHARED_SERVICE, violations=violations)

    # 规则 4：入口宿主文件只直接扫描。
    for path in HOST_FILES:
        scan_file(path, check_symbols=True, violations=violations)

    if violations:
        print("FAIL: 移动端模型设置页面存在充值功能可达引用（设计 §3.0 红线）：")
        for v in violations:
            print(f"  - {v}")
        return 1

    print(
        f"OK: 移动端页面闭包 {len(closure)} 个文件 + {len(HOST_FILES)} 个宿主文件，"
        "无充值面板 import、无充值 i18n key、无充值符号调用。"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
