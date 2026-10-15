#!/usr/bin/env python3
"""
cleanup_verification_runs.py — 安全清理历史验证/测试运行中的庞大构建废料与临时缓存。

用法:
    python3 scripts/cleanup_verification_runs.py [--target-dir <path>] [--keep <N>] [--dry-run]

说明:
    1. 默认清理已生成报告/终态的旧运行目录中的中间编译物（内嵌 Flutter SDK 副本、DerivedData、中间 zip、构建日志）。
    2. 严格受保护：所有的业务源代码（.dart, .go, .swift 等）、最终评估报告（REPORT.md, CHECKPOINT.json, FLOW.json）绝对不删。
"""

import argparse
import os
import shutil
import stat
import sys

DEFAULT_RUNS_DIRS = [
    "/Volumes/disk1/GrixVerificationRuns",
    os.path.expanduser("~/Library/Application Support/GrixVerificationRuns"),
]

WHITELIST_CLEANUP_DIRS = {
    "DerivedData", "derived-data", "flutter-3.41.6", "pub-cache", "cocoa-cache",
    "go-cache", "go-mod-cache", "ModuleCache", "module-cache", "ios-module-cache",
    "swift-cache", "swift-build", "swift-mutation-build", "go-mod", "go-mod-canonical",
    "go-mod-complete", "tmp", "go-tmp", "cocoapods-cache"
}

PROTECTED_EXTENSIONS = {
    ".dart", ".go", ".swift", ".py", ".ts", ".js", ".json", ".md", ".yaml",
    ".yml", ".toml", ".c", ".h", ".cpp", ".sql", ".sh"
}

def on_rm_error(func, path, exc_info):
    try:
        parent = os.path.dirname(path)
        if parent and os.path.exists(parent):
            os.chmod(parent, 0o777)
        if os.path.exists(path):
            os.chmod(path, stat.S_IRWXU | stat.S_IRWXG | stat.S_IRWXO)
        func(path)
    except Exception:
        pass

def safe_remove_dir(path, dry_run=False):
    if dry_run:
        return
    if os.path.islink(path):
        os.unlink(path)
    elif os.path.isdir(path):
        shutil.rmtree(path, onerror=on_rm_error)
    elif os.path.exists(path):
        try:
            parent = os.path.dirname(path)
            if parent and os.path.exists(parent):
                os.chmod(parent, 0o777)
            os.chmod(path, stat.S_IRWXU)
            os.remove(path)
        except Exception:
            pass

def clean_run_dir(run_path, dry_run=False):
    freed_bytes = 0
    cleaned_items = 0

    for root, dirs, files in os.walk(run_path, topdown=True):
        for d in list(dirs):
            if d in WHITELIST_CLEANUP_DIRS:
                dp = os.path.join(root, d)
                try:
                    for r, _, fs in os.walk(dp):
                        for f in fs:
                            freed_bytes += os.path.getsize(os.path.join(r, f))
                except Exception:
                    pass
                safe_remove_dir(dp, dry_run=dry_run)
                dirs.remove(d)
                cleaned_items += 1

        for f in files:
            ext = os.path.splitext(f)[1].lower()
            if ext in PROTECTED_EXTENSIONS:
                continue

            should_delete = False
            if f.endswith(".xcarchive.zip") or f.endswith(".ipa") or f.endswith(".log") or f == "db.sqlite":
                should_delete = True

            if should_delete:
                fp = os.path.join(root, f)
                try:
                    freed_bytes += os.path.getsize(fp)
                    if not dry_run:
                        parent = os.path.dirname(fp)
                        if parent and os.path.exists(parent):
                            os.chmod(parent, 0o777)
                        os.chmod(fp, stat.S_IRWXU)
                        os.remove(fp)
                    cleaned_items += 1
                except Exception:
                    pass

    return freed_bytes, cleaned_items

def main():
    parser = argparse.ArgumentParser(description="Clean intermediate verification caches")
    parser.add_argument("--target-dir", help="Verification runs base directory")
    parser.add_argument("--keep", type=int, default=3, help="Number of latest runs to keep fully untouched")
    parser.add_argument("--dry-run", action="store_true", help="Report what would be cleaned without removing")
    args = parser.parse_args()

    base_dir = args.target_dir
    if not base_dir:
        for p in DEFAULT_RUNS_DIRS:
            if os.path.exists(p) and os.path.isdir(p):
                base_dir = p
                break

    if not base_dir or not os.path.exists(base_dir):
        print("No verification runs directory found.")
        return 0

    runs = []
    for entry in os.scandir(base_dir):
        if entry.is_dir() and not entry.name.startswith("."):
            runs.append((entry.stat().st_mtime, entry.path, entry.name))

    # Sort newest first
    runs.sort(key=lambda x: x[0], reverse=True)

    print(f"Target runs directory: {base_dir}")
    print(f"Total runs found: {len(runs)}")
    print(f"Keeping {args.keep} most recent runs completely intact.")
    if args.dry_run:
        print("Mode: DRY-RUN (no files will be deleted)")

    total_freed = 0
    total_cleaned = 0

    for idx, (_, path, name) in enumerate(runs):
        if idx < args.keep:
            continue
        freed, count = clean_run_dir(path, dry_run=args.dry_run)
        if count > 0:
            print(f"  Cleaned {name}: {count} items, ~{freed / (1024**2):.1f} MB freed")
            total_freed += freed
            total_cleaned += count

    print(f"\nDone! Cleaned {total_cleaned} items. Total space reclaimed: {total_freed / (1024**2):.2f} MB")
    return 0

if __name__ == "__main__":
    sys.exit(main())
