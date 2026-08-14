#!/usr/bin/env python3
"""Verify app build id selection logic used by web/flutter_bootstrap.js."""


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

    print("verify_web_build_id_logic: ok")


if __name__ == "__main__":
    main()
