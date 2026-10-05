#!/usr/bin/env python3
"""Verify every vendored OCR runtime/model byte against the reviewed manifest."""
import hashlib
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1] / "web/static/ocr"


def verify():
    manifest = json.loads((ROOT / "manifest.json").read_text())
    expected = set()
    for entry in manifest["files"]:
        path = (ROOT / entry["path"]).resolve()
        if ROOT.resolve() not in path.parents or path in expected:
            raise SystemExit("Invalid or duplicate OCR resource path")
        expected.add(path)
        data = path.read_bytes()
        if len(data) != entry["bytes"] or hashlib.sha256(data).hexdigest() != entry["sha256"]:
            raise SystemExit("OCR resource mismatch: " + entry["path"])
        if not entry["license"] or not entry["source"]:
            raise SystemExit("Missing OCR resource attribution")
    actual = {path.resolve() for path in (ROOT / "tesseract-7.0.0").rglob("*") if path.is_file()}
    if expected != actual:
        raise SystemExit("Unlisted or missing OCR resources")
    for language in ("eng", "chi_sim", "chi_tra", "jpn"):
        if (ROOT / "tesseract-7.0.0/lang" / (language + ".traineddata.gz")).resolve() not in expected:
            raise SystemExit("Missing OCR language: " + language)
    print("OCR resources verified:", len(expected), "files")


if __name__ == "__main__":
    verify()
