"""URL allow-list checks to reduce SSRF risk."""

from urllib.parse import urlparse


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
