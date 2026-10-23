#!/usr/bin/env python3
"""Upload a release AAB to Google Play via the Android Publisher API.

Credentials stay outside the repo. Resolve order:
  1. --sa / PLAY_SERVICE_ACCOUNT_JSON
  2. ~/.config/grix/secrets/play-upload.json
  3. ~/.config/grix/secrets/girx-af7b7-e05432727228.json  (legacy local name)

Examples:
  frontend/scripts/android_upload_play.py --aab path/to/app-release.aab
  frontend/scripts/android_upload_play.py --aab app-release.aab --track internal --status draft
"""

from __future__ import annotations

import argparse
import os
import sys
from pathlib import Path

DEFAULT_PACKAGE = "pub.dhf.grix"
DEFAULT_TRACK = "internal"
DEFAULT_STATUS = "completed"
SA_CANDIDATES = (
    Path.home() / ".config/grix/secrets/play-upload.json",
    Path.home() / ".config/grix/secrets/girx-af7b7-e05432727228.json",
)


def resolve_sa(explicit: str | None) -> Path:
    if explicit:
        path = Path(explicit).expanduser()
        if not path.is_file():
            raise SystemExit(f"service account JSON not found: {path}")
        return path
    env = (os.environ.get("PLAY_SERVICE_ACCOUNT_JSON") or "").strip()
    if env:
        path = Path(env).expanduser()
        if not path.is_file():
            raise SystemExit(f"PLAY_SERVICE_ACCOUNT_JSON not found: {path}")
        return path
    for candidate in SA_CANDIDATES:
        if candidate.is_file():
            return candidate
    raise SystemExit(
        "missing Play service account JSON; set PLAY_SERVICE_ACCOUNT_JSON "
        "or place it at ~/.config/grix/secrets/play-upload.json"
    )


def upload(
    *,
    aab: Path,
    package: str,
    track: str,
    status: str,
    sa: Path,
    name: str | None,
) -> None:
    try:
        from google.oauth2 import service_account
        from googleapiclient.discovery import build
        from googleapiclient.errors import HttpError
        from googleapiclient.http import MediaFileUpload
    except ImportError as exc:
        raise SystemExit(
            "missing deps: pip install --user google-api-python-client google-auth"
        ) from exc

    if not aab.is_file():
        raise SystemExit(f"AAB not found: {aab}")

    creds = service_account.Credentials.from_service_account_file(
        str(sa),
        scopes=["https://www.googleapis.com/auth/androidpublisher"],
    )
    svc = build("androidpublisher", "v3", credentials=creds, cache_discovery=False)
    edit_id = svc.edits().insert(body={}, packageName=package).execute()["id"]
    print(f"edit={edit_id} package={package} sa={sa.name}", flush=True)
    try:
        media = MediaFileUpload(str(aab), mimetype="application/octet-stream", resumable=True)
        bundle = (
            svc.edits()
            .bundles()
            .upload(packageName=package, editId=edit_id, media_body=media)
            .execute()
        )
        version_code = str(bundle["versionCode"])
        print(f"uploaded versionCode={version_code} aab={aab}", flush=True)

        release: dict = {"status": status, "versionCodes": [version_code]}
        if name:
            release["name"] = name
        track_body = (
            svc.edits()
            .tracks()
            .update(
                packageName=package,
                editId=edit_id,
                track=track,
                body={"track": track, "releases": [release]},
            )
            .execute()
        )
        commit = svc.edits().commit(packageName=package, editId=edit_id).execute()
        print(
            f"committed edit={commit.get('id')} track={track} status={status}",
            flush=True,
        )
        releases = track_body.get("releases") or []
        for rel in releases:
            print(
                f"release name={rel.get('name')} versionCodes={rel.get('versionCodes')} "
                f"status={rel.get('status')}",
                flush=True,
            )
    except HttpError as exc:
        try:
            svc.edits().delete(packageName=package, editId=edit_id).execute()
        except Exception:
            pass
        reason = exc._get_reason()
        body = ""
        try:
            body = exc.content.decode()[:1000]
        except Exception:
            pass
        print(f"FAIL {exc.resp.status}: {reason}", file=sys.stderr)
        if body:
            print(body, file=sys.stderr)
        raise SystemExit(1) from exc


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aab", required=True, help="Path to app-release.aab")
    parser.add_argument("--package", default=DEFAULT_PACKAGE)
    parser.add_argument("--track", default=DEFAULT_TRACK, help="internal|alpha|beta|production")
    parser.add_argument(
        "--status",
        default=DEFAULT_STATUS,
        choices=("completed", "draft", "halted", "inProgress"),
    )
    parser.add_argument("--sa", default=None, help="Service account JSON path")
    parser.add_argument("--name", default=None, help="Release name in Play Console")
    args = parser.parse_args()

    upload(
        aab=Path(args.aab).expanduser().resolve(),
        package=args.package,
        track=args.track,
        status=args.status,
        sa=resolve_sa(args.sa),
        name=args.name,
    )


if __name__ == "__main__":
    main()
