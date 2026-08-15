#!/usr/bin/env python3
"""移动端 release 产物充值字符串扫描（M4）。

与 check_mobile_gateway_isolation.py（源码级）互补：源码隔离防"写得出来"，
本扫描防"编得进去"——对移动端 release 构建产物做 grep 级字符串扫描，断言
tree-shaking 后无充值相关 i18n key 与中英文文案残留。

用法：
  python3 scripts/tests/check_mobile_gateway_artifact_scan.py [产物目录]
  - 产物目录省略时默认 frontend/build/app/outputs/flutter-apk（Android APK 输出）。
    iOS 指到 build/ios/iphoneos（或 archive 展开目录），Web 指到 build/web。
  - 目录不存在或为空：打印 SKIP 并 exit 0（CI 尚无 release 构建步骤时的占位行为；
    接入 release 构建后把路径指到真实产物目录即自动生效）。
  - 命中充值相关字符串：逐条列出并 exit 1，应阻断构建。

已知边界（不过度设计，有意保留）：
  - assets/i18n/*.json 是全量翻译资源，按设计随包发布，不参与扫描；
  - 当前桌面面板代码仍会被编进移动端包（路由表无条件 import），因此本扫描
    一旦指向真实产物会先持续命中——这正是它的职责：倒逼后续把桌面面板
    从移动端构建中做 build-time 剔除后本门禁转绿。
"""

import sys
from pathlib import Path

# 充值/余额/流水相关：i18n key（编译进二进制的字符串字面量）+ 中英文案
# （keys 与 isolation 脚本保持一致；文案覆盖 zh/en 两个完整翻译语言）。
PATTERNS = [
    # i18n key 前缀/字面量
    "gateway_relay_topup",
    "gateway_relay_channel_",
    "gateway_relay_amount_",
    "gateway_relay_go_pay",
    "gateway_relay_pay_",
    "gateway_relay_balance_",
    "gateway_relay_invalid_amount",
    "gateway_relay_admin_topup",
    "gateway_relay_empty_topups",
    "gateway_relay_tab_topups",
    "gateway_relay_tab_ledger",
    "gateway_relay_empty_ledger",
    # 中文文案
    "充值",
    "去支付",
    "大模型余额",
    "消费流水",
    # 英文文案
    "Top Up",
    "Top-up",
    "LLM Balance",
    "Usage History",
]

# 全量翻译资源：任何 key/文案都会出现，非代码残留，跳过。
SKIP_PARTS = ("assets/i18n/", "flutter_assets/assets/i18n/")

DEFAULT_ARTIFACT_DIR = "frontend/build/app/outputs/flutter-apk"


def iter_files(root: Path):
    for path in sorted(root.rglob("*")):
        if not path.is_file():
            continue
        normalized = path.as_posix()
        if any(part in normalized for part in SKIP_PARTS):
            continue
        yield path


def main() -> int:
    repo_root = Path(__file__).resolve().parents[2]
    artifact = Path(sys.argv[1]) if len(sys.argv) > 1 else repo_root / DEFAULT_ARTIFACT_DIR
    if not artifact.is_absolute():
        artifact = repo_root / artifact

    if not artifact.is_dir() or not any(artifact.iterdir()):
        print(f"SKIP: 产物目录不存在或为空（{artifact}），未执行扫描。")
        return 0

    hits: list[str] = []
    for path in iter_files(artifact):
        try:
            data = path.read_bytes()
        except OSError:
            continue
        for pattern in PATTERNS:
            if pattern.encode("utf-8") in data:
                hits.append(f"{path.relative_to(artifact)}: 命中 '{pattern}'")

    if hits:
        print("FAIL: 移动端 release 产物中存在充值相关字符串残留（设计 §3.0 红线）：")
        for h in hits:
            print(f"  - {h}")
        return 1

    print(f"OK: {artifact} 扫描通过，无充值相关字符串残留。")
    return 0


if __name__ == "__main__":
    sys.exit(main())
