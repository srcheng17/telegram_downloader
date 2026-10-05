#!/usr/bin/env python3
"""Offline, stdlib-only feasibility probe; never logs source text or downloads files."""

import argparse
import json
import os
import re
import unicodedata
from urllib.parse import urlsplit


TITLE = re.compile(r"^《([^》]+)》$")
CAPTION = re.compile(r"^\[([^\[\]]+)\]([^\[\]]+)\[([^\[\]]+)\]$")
CREDIT = re.compile(r"^([^()]+)\(([^()]+)\)$")
TAG_LINE = re.compile(r"(?:#[^\s#]+\s*)+")
ALLOWED_HOSTS = {"telegra.ph", "www.telegra.ph", "graph.org", "www.graph.org"}


def normalized(value):
    return " ".join(unicodedata.normalize("NFKC", value).split())


def text_lines(message):
    return [line.strip() for line in message.get("text", "").splitlines()]


def heading(message):
    lines = text_lines(message)
    match = TITLE.fullmatch(next((line for line in lines if line), ""))
    return normalized(match[1]) if match else None


def zip_variants(message):
    filename = message.get("file", "")
    if not isinstance(filename, str) or not filename.lower().endswith(".zip"):
        return []
    variants = []
    for line in text_lines(message):
        caption = CAPTION.fullmatch(unicodedata.normalize("NFKC", line))
        if not caption:
            continue
        credit = CREDIT.fullmatch(caption[1].replace("#", "").strip())
        artist = normalized(credit[2]) if credit else None
        variants.append({"title": normalized(caption[2]), "artist": artist})
    return variants


def allowed_url(value):
    if not isinstance(value, str) or any(char.isspace() or ord(char) < 32 for char in value):
        return False
    try:
        url = urlsplit(value)
        return (url.scheme in {"https", "http"} and url.hostname in ALLOWED_HOSTS
                and url.username is None and url.password is None and url.port is None)
    except ValueError:
        return False


def preview(export, target):
    messages = export["messages"]
    if not isinstance(messages, list) or any(not isinstance(m.get("id"), int) for m in messages):
        raise ValueError("invalid message IDs")
    targets = [m for m in messages if m["id"] == target]
    if len(targets) != 1:
        raise ValueError("target must occur exactly once")
    intro = targets[0]
    candidate = {"id": str(target), "message_ids": [target], "metadata": {}, "evidence": {}, "warnings": []}
    result = {"source": {"channel_id": export["id"], "message_id": target},
              "outcome": "ready", "candidates": [candidate],
              "zip_candidates": [], "link_candidates": []}

    def warn(code, ids):
        candidate["warnings"].append({"code": code, "message_ids": ids})

    def put(field, value, ids, rule):
        if value:
            encoded = value.encode("utf-8")
            if len(encoded) > 4000:
                value = encoded[:4000].decode("utf-8", "ignore")
                warn("field_truncated:" + field, ids)
            candidate["metadata"][field] = value
            candidate["evidence"][field] = {"message_ids": ids, "rule": rule}

    title = heading(intro)
    if not title:
        result["outcome"] = "empty"
        warn("target_explicit_title_missing", [target])
        result["warnings"] = candidate["warnings"]
        result["candidates"] = []
        return result
    put("comic_name", title, [target], "intro_book_title")
    lines = text_lines(intro)
    while lines and not lines[0]:
        lines.pop(0)
    lines = lines[1:]
    tags = []
    while lines and (not lines[-1] or TAG_LINE.fullmatch(lines[-1])):
        tags[:0] = re.findall(r"#([^\s#]+)", lines.pop())
    put("summary", "\n".join(lines).strip(), [target], "intro_body_without_title_and_trailing_tags")
    put("tags", ",".join(dict.fromkeys(tags)), [target], "trailing_hashtag_lines")

    grouped_id = (intro.get("raw") or {}).get("GroupedID")
    if grouped_id:
        result["album"] = {"grouped_id": grouped_id, "rule": "same_grouped_id_album_only",
                           "message_ids": [m["id"] for m in messages
                                           if (m.get("raw") or {}).get("GroupedID") == grouped_id]}

    for message in messages:
        variants = zip_variants(message)
        if not any(v["title"] == title for v in variants):
            continue
        artists = {v["artist"] for v in variants}
        author = next(iter(artists)) if len(artists) == 1 and None not in artists else None
        zip_candidate = {"message_id": message["id"], "rule": "zip_caption_exact_title",
                         "artist_consistent": bool(author)}
        if author:
            zip_candidate["author"] = author
        else:
            warn("zip_artist_missing_or_conflicting", [message["id"]])
        result["zip_candidates"].append(zip_candidate)
    zip_ids = [c["message_id"] for c in result["zip_candidates"]]
    if len(zip_ids) == 1:
        candidate["message_ids"].append(zip_ids[0])
        author = result["zip_candidates"][0].get("author")
        put("author", author, [target, zip_ids[0]], "unique_zip_title_and_consistent_circle_artist")
    elif len(zip_ids) > 1:
        result["outcome"] = "needs_selection"
        warn("multiple_zip_title_matches", zip_ids)
    else:
        warn("zip_title_match_missing", [target])

    for message in messages:
        if heading(message) != title:
            continue
        urls = {entity["URL"] for entity in (message.get("raw") or {}).get("Entities", [])
                if isinstance(entity, dict) and allowed_url(entity.get("URL"))}
        for url in sorted(urls):
            result["link_candidates"].append({"message_id": message["id"], "url": url,
                                               "rule": "same_title_hidden_allowed_url"})
    if len(result["link_candidates"]) == 1:
        link = result["link_candidates"][0]
        candidate["download_url"] = link["url"]
        if link["message_id"] not in candidate["message_ids"]:
            candidate["message_ids"].append(link["message_id"])
        candidate["evidence"]["download_url"] = {"message_ids": [target, link["message_id"]],
                                                  "rule": link["rule"]}
    elif len(result["link_candidates"]) > 1:
        result["outcome"] = "needs_selection"
        warn("multiple_same_title_links", [c["message_id"] for c in result["link_candidates"]])
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", required=True)
    parser.add_argument("--target", type=int, required=True)
    parser.add_argument("--output", required=True, help="new private JSON file; must not already exist")
    args = parser.parse_args()
    try:
        with open(args.input, encoding="utf-8") as source:
            export = json.load(source)
        result = preview(export, args.target)
        fd = os.open(args.output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as output:
            json.dump(result, output, ensure_ascii=False, indent=2)
            output.write("\n")
        candidate = result["candidates"][0] if result["candidates"] else {}
        print(json.dumps({"outcome": result["outcome"], "fields": sorted(candidate.get("metadata", {})),
                          "target_id": args.target, "messages_examined": len(export["messages"]),
                          "zip_ids": [c["message_id"] for c in result["zip_candidates"]],
                          "link_ids": [c["message_id"] for c in result["link_candidates"]],
                          "has_download_url": "download_url" in candidate,
                          "warning_count": len(candidate.get("warnings", result.get("warnings", [])))}))
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        parser.exit(1, "Probe failed: check input structure, target ID, and a new writable output path.\n")


if __name__ == "__main__":
    main()
