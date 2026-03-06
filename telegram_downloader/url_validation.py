"""URL allow-list checks to reduce SSRF risk."""

from urllib.parse import ParseResult, urlparse, urlunparse


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
    """Normalize Telegraph URL for duplicate-task comparisons."""
    try:
        parsed = urlparse((raw_url or "").strip())
    except ValueError:
        return ""

    if parsed.scheme.lower() not in {"http", "https"}:
        return ""
    if not parsed.hostname:
        return ""

    host = parsed.hostname.lower()
    if host.startswith("www."):
        host = host[4:]

    path = parsed.path or "/"
    while "//" in path:
        path = path.replace("//", "/")
    if path != "/":
        path = path.rstrip("/") or "/"

    normalized = ParseResult(
        scheme="https",
        netloc=host,
        path=path,
        params="",
        query="",
        fragment="",
    )
    return urlunparse(normalized)
