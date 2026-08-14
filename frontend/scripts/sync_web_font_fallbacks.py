#!/usr/bin/env python3

from __future__ import annotations

import json
import re
import shutil
import sys
import time
import urllib.request
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
WEB_ROOT = REPO_ROOT / "frontend" / "web"
OUTPUT_ROOT = WEB_ROOT / "font-fallbacks"
MANIFEST_PATH = WEB_ROOT / "font-fallback-manifest.json"
FONT_FALLBACK_DATA_RELATIVE = Path(
    "bin/cache/flutter_web_sdk/lib/_engine/engine/font_fallback_data.dart"
)
CANVASKIT_FONTS_RELATIVE = Path(
    "bin/cache/flutter_web_sdk/lib/_engine/engine/canvaskit/fonts.dart"
)
SKWASM_FONTS_RELATIVE = Path(
    "bin/cache/flutter_web_sdk/lib/_skwasm_impl/skwasm_impl/font_collection.dart"
)
FONT_BASE_URL = "https://fonts.gstatic.com/s/"
FONT_URL_PATTERN = re.compile(r"'([^']+\.woff2)'")
ROBOTO_PATTERN = re.compile(r"'[^']*?(roboto/v\d+/[^']+\.woff2)'")
# 需要下载并打包的字体家族：覆盖官方支持的 11 种界面语言所用的全部文字系统。
# - 拉丁字母(英/德/法/西/葡)：由基础 Noto Sans 与 Roboto 覆盖。
# - CJK(中/日/韩)：简体 SC / 繁体 TC / 香港 HK / 日文 JP / 韩文 KR 全部纳入。
# - 西里尔(俄语)、希腊：由基础 Noto Sans(notosans) 覆盖。
# - 阿拉伯语：notosansarabic。
# - 印地语(天城文)：notosansdevanagari。
# 以及 emoji 与符号。这样引擎无论按字符选中哪个家族，本地都有对应字体，不会再 404。
SYNC_FONT_PREFIXES = (
    "notocoloremoji/",
    "notosans/",
    "notosanssc/",
    "notosanstc/",
    "notosanshk/",
    "notosansjp/",
    "notosanskr/",
    "notosansarabic/",
    "notosansdevanagari/",
    "notosanssymbols/",
    "notosanssymbols2/",
)
# 需要在首屏预取的字体家族：仅保留最常用的简体中文与符号/emoji，避免每次开页
# 都去拉取全部 CJK 家族（约数百个分片）。未预取的家族在引擎实际需要时按需加载，
# 因为已经打包在本地，不会 404。
PREFETCH_FONT_PREFIXES = (
    "notocoloremoji/",
    "notosanssc/",
    "notosanssymbols/",
    "notosanssymbols2/",
)


def main() -> int:
    flutter_root = resolve_flutter_root()
    sync_font_urls, prefetch_font_urls = collect_font_urls(flutter_root)
    sync_font_directory(sync_font_urls)
    write_manifest(prefetch_font_urls)
    return 0


def resolve_flutter_root() -> Path:
    flutter_bin = shutil.which("flutter")
    if flutter_bin is None:
        raise RuntimeError("flutter executable not found in PATH")
    return Path(flutter_bin).resolve().parents[1]


def collect_font_urls(flutter_root: Path) -> tuple[list[str], list[str]]:
    font_fallback_data = read_text(
        flutter_root / FONT_FALLBACK_DATA_RELATIVE,
        "Flutter Web fallback font data file",
    )
    canvaskit_fonts = read_text(
        flutter_root / CANVASKIT_FONTS_RELATIVE,
        "Flutter Web CanvasKit font file",
    )
    skwasm_fonts = read_text(
        flutter_root / SKWASM_FONTS_RELATIVE,
        "Flutter Web skwasm font file",
    )

    sync_urls = {
        url
        for url in FONT_URL_PATTERN.findall(font_fallback_data)
        if has_font_prefix(url, SYNC_FONT_PREFIXES)
    }
    prefetch_urls = {
        url for url in sync_urls if has_font_prefix(url, PREFETCH_FONT_PREFIXES)
    }

    roboto_urls = set(ROBOTO_PATTERN.findall(canvaskit_fonts))
    roboto_urls.update(ROBOTO_PATTERN.findall(skwasm_fonts))

    sync_urls.update(roboto_urls)
    prefetch_urls.update(roboto_urls)

    if not sync_urls:
        raise RuntimeError("no fallback font URLs found in Flutter SDK")

    return sorted(sync_urls), sorted(prefetch_urls)


def has_font_prefix(url: str, prefixes: tuple[str, ...]) -> bool:
    return any(url.startswith(prefix) for prefix in prefixes)


def read_text(path: Path, description: str) -> str:
    if not path.is_file():
        raise RuntimeError(f"{description} not found: {path}")
    return path.read_text(encoding="utf-8")


def sync_font_directory(font_urls: list[str]) -> None:
    OUTPUT_ROOT.mkdir(parents=True, exist_ok=True)

    # 增量同步：已存在的字体保留并跳过，避免每次发布都全量删除重下。
    expected = set()
    for relative_url in font_urls:
        destination = OUTPUT_ROOT / relative_url
        expected.add(destination)
        if destination.is_file():
            continue
        destination.parent.mkdir(parents=True, exist_ok=True)
        download_font(relative_url, destination)

    # 仅清理 SDK 已不再声明的陈旧字体，保持目录与声明集一致；
    # SDK 未变时此处不会删除任何文件。
    for path in OUTPUT_ROOT.rglob("*.woff2"):
        if path not in expected:
            path.unlink()


def download_font(relative_url: str, destination: Path) -> None:
    source_url = FONT_BASE_URL + relative_url
    last_error: Exception | None = None
    for attempt in range(5):
        try:
            with urllib.request.urlopen(source_url, timeout=60) as response:
                destination.write_bytes(response.read())
            return
        except Exception as error:  # 瞬时网络抖动重试，避免单次中断使全量下载失败
            last_error = error
            time.sleep(2 * (attempt + 1))
    raise RuntimeError(f"failed to download {source_url}: {last_error}")


def write_manifest(font_urls: list[str]) -> None:
    manifest = {
        "baseUrl": "font-fallbacks/",
        "prefetch": font_urls,
        "generatedBy": "frontend/scripts/sync_web_font_fallbacks.py",
    }
    MANIFEST_PATH.write_text(
        json.dumps(manifest, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # pragma: no cover - operational failure path
        print(f"sync_web_font_fallbacks.py: {exc}", file=sys.stderr)
        raise SystemExit(1)
