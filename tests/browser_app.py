"""Isolated browser fixture; production never reads fixture environment variables."""

import sys
from pathlib import Path

from telegraph_downloader.config import Config
from telegraph_downloader.main import create_app

app = create_app(Config(gallery_command=(sys.executable, str(Path(__file__).parent / "fixtures/gallery_runner.py"))))
