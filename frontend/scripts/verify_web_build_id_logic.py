#!/usr/bin/env python3
"""Verify app build id selection and bootstrap ordering."""

from pathlib import Path


def normalize_build_token(value):
    if not isinstance(value, str):
        return ""
    return value.strip()


def build_app_build_id(build_info):
    if not isinstance(build_info, dict):
        return None

    web_build_id = normalize_build_token(build_info.get("web_build_id"))
    if web_build_id:
        return web_build_id

    version = normalize_build_token(build_info.get("version"))
    build_number = normalize_build_token(build_info.get("build_number"))
    if not version or not build_number:
        return None
    return f"{version}+{build_number}"


def main():
    cases = [
        ({"version": "2.3.0", "build_number": "353"}, "2.3.0+353"),
        (
            {"version": "2.3.0", "build_number": "353", "web_build_id": "web-a"},
            "web-a",
        ),
        (
            {"version": "2.3.0", "build_number": "353", "web_build_id": "  web-b  "},
            "web-b",
        ),
        ({"version": "2.3.0", "build_number": ""}, None),
        ({}, None),
        (None, None),
    ]

    for idx, (payload, expected) in enumerate(cases, start=1):
        actual = build_app_build_id(payload)
        if actual != expected:
            raise SystemExit(
                f"case {idx} failed: payload={payload!r}, expected={expected!r}, actual={actual!r}"
            )

    bootstrap_path = Path(__file__).parents[1] / "web" / "flutter_bootstrap.js"
    bootstrap = bootstrap_path.read_text(encoding="utf-8")
    start_marker = "async function startFlutterApp()"
    await_marker = "const buildId = await appBuildInfoPromise;"
    load_marker = "_flutter.loader.load({"
    start_index = bootstrap.find(start_marker)
    await_index = bootstrap.find(await_marker, start_index)
    load_index = bootstrap.find(load_marker, start_index)
    if min(start_index, await_index, load_index) < 0 or not (
        start_index < await_index < load_index
    ):
        raise SystemExit(
            "bootstrap ordering failed: build id must resolve before Flutter loads"
        )

    print("verify_web_build_id_logic: ok")


if __name__ == "__main__":
    main()
