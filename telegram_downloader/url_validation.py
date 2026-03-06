"""URL allow-list checks to reduce SSRF risk."""

import re
from urllib.parse import urlparse, urlunsplit


def is_allowed_telegraph_url(raw_url, allowed_hosts):
    """Allow only HTTP(S) Telegraph URLs."""
    try:
        parsed = urlparse(raw_url)
    except ValueError:
        return False
    if parsed.scheme not in {"http", "https"}:
        return False
    if not parsed.hostname:
        return False
    return parsed.hostname.lower() in allowed_hosts


def normalize_telegraph_url(raw_url):
    """
    Normalize Telegraph URLs for duplicate detection.

    The content identity is the article path; query/fragment noise and
    scheme/www variants should not create separate download jobs.
    """
    try:
        parsed = urlparse((raw_url or "").strip())
    except ValueError:
        return ""

    if parsed.scheme not in {"http", "https"} or not parsed.hostname:
        return ""

    host = parsed.hostname.lower()
    if host.startswith("www."):
        host = host[4:]

    path = parsed.path or "/"
    path = re.sub(r"/{2,}", "/", path)
    if path != "/":
        path = path.rstrip("/")

    return urlunsplit(("https", host, path, "", ""))
