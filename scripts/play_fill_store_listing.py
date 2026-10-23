#!/usr/bin/env python3
"""Fill Google Play store listing for Grix from docs/play/store_listing.json (mirrored from iOS ASC).

Usage:
  ./scripts/play_fill_store_listing.py
  ./scripts/play_fill_store_listing.py --dry-run
"""
from __future__ import annotations

import argparse
import json
import mimetypes
import os
import sys
from pathlib import Path

from google.oauth2 import service_account
from googleapiclient.discovery import build
from googleapiclient.http import MediaFileUpload

SCRIPT_DIR = Path(__file__).resolve().parent
REPO_ROOT = SCRIPT_DIR.parent
DEFAULT_META = REPO_ROOT / "docs" / "play" / "store_listing.json"
DEFAULT_GRAPHICS = REPO_ROOT / "docs" / "play" / "graphics"
DEFAULT_SHOTS = REPO_ROOT / "docs" / "play" / "screenshots"
DEFAULT_SA = Path.home() / ".config/grix/secrets/play-upload.json"
SCOPES = ["https://www.googleapis.com/auth/androidpublisher"]


def die(msg: str) -> None:
    print(f"play_fill_store_listing: {msg}", file=sys.stderr)
    raise SystemExit(1)


def log(msg: str) -> None:
    print(f"play_fill_store_listing: {msg}")


def media(path: Path) -> MediaFileUpload:
    mime = mimetypes.guess_type(path.name)[0] or "image/jpeg"
    return MediaFileUpload(str(path), mimetype=mime, resumable=True)


def shots_for(lang: str, root: Path) -> list[Path]:
    # Prefer language-specific dir; fall back to shared root.
    candidates = [
        root / lang,
        root / lang.replace("-", "_"),
        root,
    ]
    for d in candidates:
        if not d.is_dir():
            continue
        files = sorted([*d.glob("*.png"), *d.glob("*.jpg"), *d.glob("*.jpeg")])
        if files:
            return files[:8]
    return []


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--meta", default=os.environ.get("PLAY_STORE_LISTING_META", str(DEFAULT_META)))
    ap.add_argument("--credentials", default=os.environ.get("PLAY_UPLOAD_CREDENTIALS", str(DEFAULT_SA)))
    ap.add_argument("--graphics", default=str(DEFAULT_GRAPHICS))
    ap.add_argument("--screenshots", default=str(DEFAULT_SHOTS))
    ap.add_argument("--dry-run", action="store_true")
    ap.add_argument("--skip-images", action="store_true")
    ap.add_argument("--skip-release-notes", action="store_true")
    args = ap.parse_args()

    meta_path = Path(args.meta).expanduser()
    if not meta_path.is_file():
        die(f"meta not found: {meta_path}")
    meta = json.loads(meta_path.read_text())
    package = meta["packageName"]
    sa = Path(args.credentials).expanduser()
    if not sa.is_file():
        die(f"credentials not found: {sa}")

    graphics = Path(args.graphics)
    shots_root = Path(args.screenshots)
    icon = graphics / "icon-512.png"
    feature = graphics / "feature-graphic-1024x500.png"
    if not args.skip_images and (not icon.is_file() or not feature.is_file()):
        die(f"missing graphics under {graphics}")

    log(f"package={package} meta={meta_path}")
    if args.dry_run:
        for lang, listing in meta["listings"].items():
            shots = shots_for(lang, shots_root)
            log(
                f"[dry-run] {lang}: title={listing['title']!r} "
                f"short={len(listing['shortDescription'])} full={len(listing['fullDescription'])} "
                f"shots={len(shots)}"
            )
        return 0

    creds = service_account.Credentials.from_service_account_file(str(sa), scopes=SCOPES)
    svc = build("androidpublisher", "v3", credentials=creds, cache_discovery=False)
    edit = svc.edits().insert(body={}, packageName=package).execute()
    edit_id = edit["id"]
    log(f"edit={edit_id}")

    try:
        details_body = {
            "defaultLanguage": meta["defaultLanguage"],
            "contactEmail": meta["contactEmail"],
            "contactWebsite": meta["contactWebsite"],
        }
        details = (
            svc.edits()
            .details()
            .update(packageName=package, editId=edit_id, body=details_body)
            .execute()
        )
        log(f"details={details}")

        for lang, listing in meta["listings"].items():
            body = {
                "language": lang,
                "title": listing["title"],
                "shortDescription": listing["shortDescription"],
                "fullDescription": listing["fullDescription"],
            }
            updated = (
                svc.edits()
                .listings()
                .update(packageName=package, editId=edit_id, language=lang, body=body)
                .execute()
            )
            log(
                f"listing {lang}: title={updated.get('title')!r} "
                f"short={len(updated.get('shortDescription') or '')} "
                f"full={len(updated.get('fullDescription') or '')}"
            )
            if args.skip_images:
                continue

            for image_type, path in (("icon", icon), ("featureGraphic", feature)):
                svc.edits().images().deleteall(
                    packageName=package, editId=edit_id, language=lang, imageType=image_type
                ).execute()
                svc.edits().images().upload(
                    packageName=package,
                    editId=edit_id,
                    language=lang,
                    imageType=image_type,
                    media_body=media(path),
                ).execute()
                log(f"  {lang}/{image_type}: ok")

            shots = shots_for(lang, shots_root)
            if len(shots) < 2:
                die(f"need >=2 phone screenshots for {lang} under {shots_root}")
            svc.edits().images().deleteall(
                packageName=package, editId=edit_id, language=lang, imageType="phoneScreenshots"
            ).execute()
            for shot in shots:
                svc.edits().images().upload(
                    packageName=package,
                    editId=edit_id,
                    language=lang,
                    imageType="phoneScreenshots",
                    media_body=media(shot),
                ).execute()
                log(f"  {lang}/phoneScreenshots {shot.name}: ok")

        if not args.skip_release_notes and meta.get("releaseNotes"):
            track = (
                svc.edits()
                .tracks()
                .get(packageName=package, editId=edit_id, track="internal")
                .execute()
            )
            releases = track.get("releases") or []
            if releases:
                def _trim(text: str, limit: int = 500) -> str:
                    text = text.strip()
                    if len(text) <= limit:
                        return text
                    cut = text[: limit - 1].rsplit("\n", 1)[0].rstrip()
                    if len(cut) < limit * 0.6:
                        cut = text[: limit - 1].rstrip()
                    return cut + "…"

                notes = [
                    {"language": lang, "text": _trim(text)}
                    for lang, text in meta["releaseNotes"].items()
                    if text
                ]
                releases[0]["releaseNotes"] = notes
                svc.edits().tracks().update(
                    packageName=package,
                    editId=edit_id,
                    track="internal",
                    body={"track": "internal", "releases": releases},
                ).execute()
                log(f"internal releaseNotes languages={[n['language'] for n in notes]}")

        commit = svc.edits().commit(packageName=package, editId=edit_id).execute()
        log(f"committed edit id={commit.get('id')}")
        log("OK store listing filled from iOS-mirrored meta")
        if meta.get("privacyPolicyUrl"):
            log(
                f"NOTE: set Privacy Policy URL in Play Console App content → "
                f"{meta['privacyPolicyUrl']}"
            )
        log("NOTE: Content rating (17+) + Data safety still need Play Console")
        return 0
    except Exception:
        try:
            svc.edits().delete(packageName=package, editId=edit_id).execute()
            log(f"rolled back edit {edit_id}")
        except Exception as cleanup_err:  # noqa: BLE001
            log(f"edit cleanup failed: {cleanup_err}")
        raise


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SystemExit:
        raise
    except Exception as exc:  # noqa: BLE001
        die(str(exc))
