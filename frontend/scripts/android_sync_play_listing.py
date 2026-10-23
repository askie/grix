#!/usr/bin/env python3
"""Sync Google Play store listings from frontend/store/android/listings/.

Mirrors the iOS App Store copy checked into those JSON files.
Does not upload screenshots or complete App content questionnaires.

Usage:
  frontend/scripts/android_sync_play_listing.py
  frontend/scripts/android_sync_play_listing.py --dry-run
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path

PACKAGE_DEFAULT = "pub.dhf.grix"
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


def load_listings(listings_dir: Path) -> tuple[list[dict], dict]:
    details_path = listings_dir / "details.json"
    details = json.loads(details_path.read_text(encoding="utf-8")) if details_path.is_file() else {}
    listings: list[dict] = []
    for path in sorted(listings_dir.glob("*.json")):
        if path.name == "details.json":
            continue
        data = json.loads(path.read_text(encoding="utf-8"))
        for key in ("language", "title", "shortDescription", "fullDescription"):
            if not (data.get(key) or "").strip():
                raise SystemExit(f"{path.name}: missing {key}")
        if len(data["title"]) > 30:
            raise SystemExit(f"{path.name}: title > 30 chars")
        if len(data["shortDescription"]) > 80:
            raise SystemExit(f"{path.name}: shortDescription > 80 chars")
        if len(data["fullDescription"]) > 4000:
            raise SystemExit(f"{path.name}: fullDescription > 4000 chars")
        listings.append(
            {
                "language": data["language"],
                "title": data["title"],
                "shortDescription": data["shortDescription"],
                "fullDescription": data["fullDescription"],
            }
        )
    if not listings:
        raise SystemExit(f"no locale JSON found under {listings_dir}")
    return listings, details


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package", default=PACKAGE_DEFAULT)
    parser.add_argument("--sa", default=None)
    parser.add_argument(
        "--listings-dir",
        default=None,
        help="Defaults to frontend/store/android/listings",
    )
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args()

    script_dir = Path(__file__).resolve().parent
    listings_dir = (
        Path(args.listings_dir).expanduser().resolve()
        if args.listings_dir
        else (script_dir.parent / "store/android/listings").resolve()
    )
    listings, details = load_listings(listings_dir)
    print(f"listings_dir={listings_dir}")
    for item in listings:
        print(
            f"  {item['language']}: title={item['title']!r} "
            f"short={len(item['shortDescription'])} full={len(item['fullDescription'])}"
        )

    if args.dry_run:
        print("dry-run: ok")
        return

    try:
        from google.oauth2 import service_account
        from googleapiclient.discovery import build
        from googleapiclient.errors import HttpError
    except ImportError as exc:
        raise SystemExit(
            "missing deps: pip install --user google-api-python-client google-auth"
        ) from exc

    sa = resolve_sa(args.sa)
    creds = service_account.Credentials.from_service_account_file(
        str(sa),
        scopes=["https://www.googleapis.com/auth/androidpublisher"],
    )
    svc = build("androidpublisher", "v3", credentials=creds, cache_discovery=False)
    edit_id = svc.edits().insert(body={}, packageName=args.package).execute()["id"]
    print(f"edit={edit_id} sa={sa.name}")
    try:
        for body in listings:
            lang = body["language"]
            result = (
                svc.edits()
                .listings()
                .update(packageName=args.package, editId=edit_id, language=lang, body=body)
                .execute()
            )
            print(f"updated listing {lang} title={result.get('title')}")

        details_body = {
            k: details[k]
            for k in ("defaultLanguage", "contactWebsite", "contactEmail", "contactPhone")
            if details.get(k)
        }
        if details_body:
            try:
                patched = (
                    svc.edits()
                    .details()
                    .patch(packageName=args.package, editId=edit_id, body=details_body)
                    .execute()
                )
            except HttpError:
                patched = (
                    svc.edits()
                    .details()
                    .update(packageName=args.package, editId=edit_id, body=details_body)
                    .execute()
                )
            print(f"updated details {patched}")

        commit = svc.edits().commit(packageName=args.package, editId=edit_id).execute()
        print(f"committed edit={commit.get('id')}")
        if details.get("privacyPolicyUrl"):
            print(
                "note: set privacy policy in Play Console App content → "
                f"{details['privacyPolicyUrl']}"
            )
    except Exception:
        try:
            svc.edits().delete(packageName=args.package, editId=edit_id).execute()
        except Exception:
            pass
        raise


if __name__ == "__main__":
    main()
