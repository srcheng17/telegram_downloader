"""Compatibility shim for legacy imports."""

from telegram_downloader.services import image_downloader as _impl

DownloadCancelledError = _impl.DownloadCancelledError
download_images = _impl.download_images
fetch_image = _impl.fetch_image
sanitize_title = _impl.sanitize_title
zip_folder = _impl.zip_folder

# Keep legacy test hooks available.
_SESSION_LOCAL = _impl._SESSION_LOCAL
_build_paths = _impl._build_paths
time = _impl.time


def __getattr__(name):
    return getattr(_impl, name)
