# Local OCR resources

The application explicitly loads this same-origin Tesseract.js 7.0.0 worker,
Tesseract.js-core 7.0.0 LSTM cores (plain/SIMD/relaxed SIMD), and the four
`@tesseract.js-data/<language>@1.0.0` `4.0.0_best_int` models. It never uses the
vendor's default CDN paths. Missing resources fail locally. These public assets
may use the browser HTTP cache; the application disables the vendor IndexedDB
cache and never persists images or recognized text.

`manifest.json` records the exact SHA-256, bytes, source and license for every
vendored file. `python3 scripts/verify_ocr_resources.py` verifies the release.
The models originate from Tesseract tessdata under Apache-2.0; the npm packaging
declares MIT. Upstream engine/model licenses and bundled third-party notices
are included in `tesseract-7.0.0/licenses/`.

To replace resources deliberately, obtain the exact npm package versions,
extract only the matching runtime/model files, review licenses, regenerate the
manifest hashes, and rerun real-browser OCR checks. Do not regenerate the
manifest to bypass a failed integrity check.
