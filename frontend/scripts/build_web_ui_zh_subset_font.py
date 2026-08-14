#!/usr/bin/env python3

from __future__ import annotations

from dataclasses import dataclass
import re
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path


REPO_ROOT = Path(__file__).resolve().parents[2]
FRONTEND_ROOT = REPO_ROOT / "frontend"
LIB_ROOT = FRONTEND_ROOT / "lib"
SOURCE_FONT_ROOT = FRONTEND_ROOT / "web" / "font-fallbacks" / "notosanssc"
OUTPUT_FONT_PATH = FRONTEND_ROOT / "assets" / "fonts" / "grix_ui_zh_subset.ttf"
GB2312_LEVEL1_ROW_RANGE = range(16, 56)
TARGET_CODEPOINT_RANGES = (
    (0x00B7, 0x00B7),
    (0x2000, 0x206F),
    (0x3000, 0x303F),
    (0x3400, 0x4DBF),
    (0x4E00, 0x9FFF),
    (0xF900, 0xFAFF),
    (0xFF00, 0xFFEF),
)
CMAP_CODE_PATTERN = re.compile(r'code="0x([0-9A-Fa-f]+)"')


@dataclass(frozen=True)
class Toolchain:
    ttx: Path
    pyftsubset: Path
    fonttools_python: Path


@dataclass(frozen=True)
class FontShard:
    path: Path
    size_bytes: int
    coverage: frozenset[int]


def main() -> int:
    toolchain = resolve_toolchain()
    required_codepoints = collect_required_codepoints()
    source_font_paths = discover_source_fonts()
    font_shards = load_font_shards(toolchain, source_font_paths, required_codepoints)
    selected_shards = select_font_shards(required_codepoints, font_shards)
    build_subset_font(toolchain, selected_shards, required_codepoints)
    output_size_kib = OUTPUT_FONT_PATH.stat().st_size / 1024
    print(
        "built web UI zh subset font: "
        f"{OUTPUT_FONT_PATH} "
        f"({len(required_codepoints)} codepoints, "
        f"{len(selected_shards)} shards, "
        f"{output_size_kib:.1f} KiB)"
    )
    return 0


def resolve_toolchain() -> Toolchain:
    missing_tools = [
        tool_name
        for tool_name in ("ttx", "pyftmerge", "pyftsubset")
        if shutil.which(tool_name) is None
    ]
    if missing_tools:
        missing_list = ", ".join(missing_tools)
        raise RuntimeError(
            f"missing required fontTools executables: {missing_list}"
        )

    pyftmerge_path = Path(shutil.which("pyftmerge") or "")
    fonttools_python = resolve_fonttools_python(pyftmerge_path)

    return Toolchain(
        ttx=Path(shutil.which("ttx") or ""),
        pyftsubset=Path(shutil.which("pyftsubset") or ""),
        fonttools_python=fonttools_python,
    )


def resolve_fonttools_python(pyftmerge_path: Path) -> Path:
    first_line = pyftmerge_path.read_text(encoding="utf-8").splitlines()[0]
    if not first_line.startswith("#!"):
        return Path(sys.executable)
    python_path = Path(first_line[2:].strip())
    if not python_path.is_file():
        raise RuntimeError(
            f"fontTools Python interpreter not found: {python_path}"
        )
    return python_path


def collect_required_codepoints() -> set[int]:
    required_codepoints = collect_ui_codepoints()
    required_codepoints.update(collect_common_dynamic_han_codepoints())
    if not required_codepoints:
        raise RuntimeError(f"no target codepoints found under {LIB_ROOT}")
    return required_codepoints


def collect_ui_codepoints() -> set[int]:
    required_codepoints: set[int] = set()
    for source_path in sorted(LIB_ROOT.rglob("*.dart")):
        source_text = source_path.read_text(encoding="utf-8")
        for character in source_text:
            codepoint = ord(character)
            if is_target_codepoint(codepoint):
                required_codepoints.add(codepoint)
    return required_codepoints


def collect_common_dynamic_han_codepoints() -> set[int]:
    codepoints: set[int] = set()
    for row in GB2312_LEVEL1_ROW_RANGE:
        for column in range(1, 95):
            gb2312_bytes = bytes((0xA0 + row, 0xA0 + column))
            try:
                character = gb2312_bytes.decode("gb2312")
            except UnicodeDecodeError:
                continue
            if len(character) != 1:
                continue
            codepoint = ord(character)
            if 0x4E00 <= codepoint <= 0x9FFF:
                codepoints.add(codepoint)
    return codepoints


def is_target_codepoint(codepoint: int) -> bool:
    return any(start <= codepoint <= end for start, end in TARGET_CODEPOINT_RANGES)


def discover_source_fonts() -> list[Path]:
    if not SOURCE_FONT_ROOT.is_dir():
        raise RuntimeError(
            f"source fallback fonts not found: {SOURCE_FONT_ROOT}. "
            "Run scripts/sync_web_font_fallbacks.py first."
        )

    source_font_paths = sorted(SOURCE_FONT_ROOT.rglob("*.woff2"))
    if not source_font_paths:
        raise RuntimeError(f"no source fallback fonts found under {SOURCE_FONT_ROOT}")
    return source_font_paths


def load_font_shards(
    toolchain: Toolchain,
    source_font_paths: list[Path],
    required_codepoints: set[int],
) -> list[FontShard]:
    font_shards: list[FontShard] = []
    for source_font_path in source_font_paths:
        available_codepoints = extract_font_codepoints(toolchain.ttx, source_font_path)
        covered_codepoints = frozenset(available_codepoints & required_codepoints)
        if not covered_codepoints:
            continue
        font_shards.append(
            FontShard(
                path=source_font_path,
                size_bytes=source_font_path.stat().st_size,
                coverage=covered_codepoints,
            )
        )

    if not font_shards:
        raise RuntimeError(
            f"no source fonts cover target UI codepoints under {SOURCE_FONT_ROOT}"
        )

    return font_shards


def extract_font_codepoints(ttx_path: Path, font_path: Path) -> set[int]:
    command = [str(ttx_path), "-q", "-t", "cmap", "-o", "-", str(font_path)]
    stdout = run_command(command)
    return {int(match, 16) for match in CMAP_CODE_PATTERN.findall(stdout)}


def select_font_shards(
    required_codepoints: set[int],
    font_shards: list[FontShard],
) -> list[FontShard]:
    uncovered_codepoints = set(required_codepoints)
    remaining_shards = list(font_shards)
    selected_shards: list[FontShard] = []

    while uncovered_codepoints:
        best_shard: FontShard | None = None
        best_gain: frozenset[int] = frozenset()

        for font_shard in remaining_shards:
            gain = font_shard.coverage & uncovered_codepoints
            if not gain:
                continue
            if best_shard is None or is_better_shard(font_shard, gain, best_shard, best_gain):
                best_shard = font_shard
                best_gain = frozenset(gain)

        if best_shard is None:
            raise RuntimeError(
                "unable to cover all required UI codepoints with local Noto Sans SC shards: "
                f"{format_missing_codepoints(uncovered_codepoints)}"
            )

        selected_shards.append(best_shard)
        uncovered_codepoints -= best_gain
        remaining_shards.remove(best_shard)

    return selected_shards


def is_better_shard(
    candidate: FontShard,
    candidate_gain: frozenset[int],
    incumbent: FontShard,
    incumbent_gain: frozenset[int],
) -> bool:
    if len(candidate_gain) != len(incumbent_gain):
        return len(candidate_gain) > len(incumbent_gain)
    if candidate.size_bytes != incumbent.size_bytes:
        return candidate.size_bytes < incumbent.size_bytes
    return candidate.path.as_posix() < incumbent.path.as_posix()


def format_missing_codepoints(codepoints: set[int]) -> str:
    preview = "".join(chr(codepoint) for codepoint in sorted(codepoints)[:16])
    preview_hex = ", ".join(f"U+{codepoint:04X}" for codepoint in sorted(codepoints)[:16])
    return f"{preview_hex} ({preview})"


def build_subset_font(
    toolchain: Toolchain,
    selected_shards: list[FontShard],
    required_codepoints: set[int],
) -> None:
    OUTPUT_FONT_PATH.parent.mkdir(parents=True, exist_ok=True)
    required_characters = "".join(chr(codepoint) for codepoint in sorted(required_codepoints))

    with tempfile.TemporaryDirectory(prefix="grix-ui-zh-font-") as temp_dir_name:
        temp_dir = Path(temp_dir_name)
        merged_font_path = temp_dir / "merged.ttf"
        required_characters_path = temp_dir / "required_chars.txt"
        required_characters_path.write_text(required_characters, encoding="utf-8")

        merge_selected_fonts(
            toolchain,
            [font_shard.path for font_shard in selected_shards],
            merged_font_path,
        )
        subset_merged_font(
            toolchain,
            merged_font_path,
            required_characters_path,
            OUTPUT_FONT_PATH,
        )


def merge_selected_fonts(
    toolchain: Toolchain,
    input_font_paths: list[Path],
    output_font_path: Path,
) -> None:
    merge_script = (
        "from fontTools.merge import Merger\n"
        "import sys\n"
        "font = Merger().merge(sys.argv[1:-1])\n"
        "font.save(sys.argv[-1])\n"
    )
    run_command(
        [
            str(toolchain.fonttools_python),
            "-c",
            merge_script,
            *(str(path) for path in input_font_paths),
            str(output_font_path),
        ]
    )


def subset_merged_font(
    toolchain: Toolchain,
    merged_font_path: Path,
    required_characters_path: Path,
    output_font_path: Path,
) -> None:
    run_command(
        [
            str(toolchain.pyftsubset),
            str(merged_font_path),
            f"--text-file={required_characters_path}",
            "--no-ignore-missing-unicodes",
            f"--output-file={output_font_path}",
        ]
    )


def run_command(command: list[str]) -> str:
    completed = subprocess.run(
        command,
        check=False,
        capture_output=True,
        text=True,
    )
    if completed.returncode != 0:
        stderr = completed.stderr.strip()
        stdout = completed.stdout.strip()
        detail = stderr or stdout or "unknown error"
        raise RuntimeError(f"command failed: {' '.join(command)}\n{detail}")
    return completed.stdout


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # pragma: no cover - operational failure path
        print(f"build_web_ui_zh_subset_font.py: {exc}", file=sys.stderr)
        raise SystemExit(1)
