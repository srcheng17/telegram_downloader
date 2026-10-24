"""Run the real gallery-dl extractor against an offline HTTP fixture."""

import os

import gallery_dl
from gallery_dl.extractor.telegraph import TelegraphGalleryExtractor

TelegraphGalleryExtractor.root = os.environ["GALLERY_FIXTURE_ROOT"]
raise SystemExit(gallery_dl.main())
