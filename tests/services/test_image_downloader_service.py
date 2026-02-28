import os
import tempfile
import unittest
from pathlib import Path
from unittest.mock import call, patch

import requests
from bs4 import BeautifulSoup

import telegram_downloader.services.image_downloader as logic


class _FakeResponse:
    def __init__(self, chunks):
        self._chunks = chunks

    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc, tb):
        return False

    def raise_for_status(self):
        return None

    def iter_content(self, chunk_size=8192):
        del chunk_size
        for chunk in self._chunks:
            yield chunk


class _FakeSession:
    def __init__(self):
        self.calls = 0

    def get(self, url, timeout, stream):
        del url, timeout, stream
        self.calls += 1
        if self.calls < 3:
            raise requests.exceptions.Timeout("temporary timeout")
        return _FakeResponse([b"hello", b"", b"world"])

    def close(self):
        return None


class _AlwaysSuccessSession:
    def get(self, url, timeout, stream):
        del url, timeout, stream
        return _FakeResponse([b"chunk-1", b"chunk-2"])

    def close(self):
        return None


class ImageDownloaderServiceTests(unittest.TestCase):
    def tearDown(self):
        if hasattr(logic._SESSION_LOCAL, "session"):
            logic._SESSION_LOCAL.session.close()
            delattr(logic._SESSION_LOCAL, "session")
        if hasattr(logic._SESSION_LOCAL, "pool_size"):
            delattr(logic._SESSION_LOCAL, "pool_size")

    def test_fetch_image_retries_with_backoff_and_saves_content(self):
        fake_session = _FakeSession()
        logic._SESSION_LOCAL.session = fake_session
        logic._SESSION_LOCAL.pool_size = 4

        with tempfile.TemporaryDirectory() as temp_dir:
            save_path = Path(temp_dir) / "image.jpg"

            with patch("telegram_downloader.services.image_downloader.time.sleep") as sleep_mock:
                result = logic.fetch_image(
                    "https://example.com/img.jpg",
                    timeout=5,
                    retries=2,
                    save_path=str(save_path),
                    pool_size=4,
                )
            saved_bytes = save_path.read_bytes()

        self.assertEqual(result, str(save_path))
        self.assertEqual(fake_session.calls, 3)
        self.assertEqual(saved_bytes, b"helloworld")
        sleep_mock.assert_has_calls([call(0.5), call(1.0)])

    def test_build_paths_avoids_zip_name_collision(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_root = Path(temp_dir) / "tmp"
            base_root = Path(temp_dir) / "out"
            temp_root.mkdir(parents=True, exist_ok=True)
            base_root.mkdir(parents=True, exist_ok=True)
            (base_root / "Title.zip").write_bytes(b"1")
            (base_root / "Title (2).zip").write_bytes(b"1")

            temp_path, zip_path = logic._build_paths(
                str(temp_root),
                str(base_root),
                "Title",
                "abcdef123456",
            )
            _, next_zip_path = logic._build_paths(
                str(temp_root),
                str(base_root),
                "Title",
                "99999999",
            )
            reserved_file = Path(zip_path)
            next_reserved_file = Path(next_zip_path)
            self.assertTrue(reserved_file.exists())
            self.assertTrue(next_reserved_file.exists())

        self.assertTrue(temp_path.endswith("Title-abcdef12"))
        self.assertTrue(zip_path.endswith("Title (3).zip"))
        self.assertTrue(next_zip_path.endswith("Title (4).zip"))

    def test_fetch_image_honors_cancellation(self):
        logic._SESSION_LOCAL.session = _AlwaysSuccessSession()
        logic._SESSION_LOCAL.pool_size = 4

        with tempfile.TemporaryDirectory() as temp_dir:
            save_path = Path(temp_dir) / "cancel.jpg"
            with self.assertRaises(logic.DownloadCancelledError):
                logic.fetch_image(
                    "https://example.com/img.jpg",
                    timeout=5,
                    retries=2,
                    save_path=str(save_path),
                    pool_size=4,
                    should_cancel=lambda: True,
                )
            self.assertFalse(save_path.exists())

    def test_download_images_marks_partial_failures_as_failed(self):
        html = "<html><head><title>Demo</title></head><body><img src='ok.jpg'><img src='missing.jpg'></body></html>"
        soup = BeautifulSoup(html, "html.parser")
        tasks = {"task-1": {"status": "IN_PROGRESS", "progress": 0, "image_concurrency": 2}}

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            def fake_fetch_image(img_url, timeout, retries, save_path, pool_size, should_cancel=None):
                del timeout, retries, pool_size, should_cancel
                if img_url.endswith("missing.jpg"):
                    raise Exception(f"Request failed for {img_url}: 404")
                Path(save_path).write_bytes(b"ok")
                return save_path

            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch("telegram_downloader.services.image_downloader.fetch_image", side_effect=fake_fetch_image):
                        with self.assertRaises(logic.PartialDownloadError) as context:
                            logic.download_images(
                                "https://telegra.ph/demo",
                                timeout=5,
                                retries=1,
                                task_id="task-1",
                                image_concurrency=2,
                                tasks_db=tasks,
                            )

            self.assertEqual(tasks["task-1"]["total_images"], 2)
            self.assertEqual(tasks["task-1"]["progress"], 1)
            self.assertEqual(tasks["task-1"]["status"], "FAILED")
            self.assertIn("Partial download failed", str(context.exception))
            self.assertIn("Partial download failed", tasks["task-1"]["error"])

    def test_download_images_fails_when_all_images_fail(self):
        html = "<html><head><title>Demo</title></head><body><img src='a.jpg'><img src='b.jpg'></body></html>"
        soup = BeautifulSoup(html, "html.parser")
        tasks = {"task-2": {"status": "IN_PROGRESS", "progress": 0, "image_concurrency": 2}}

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch(
                        "telegram_downloader.services.image_downloader.fetch_image",
                        side_effect=Exception("Request failed: 404"),
                    ):
                        with self.assertRaises(Exception) as context:
                            logic.download_images(
                                "https://telegra.ph/demo",
                                timeout=5,
                                retries=1,
                                task_id="task-2",
                                image_concurrency=2,
                                tasks_db=tasks,
                            )

        self.assertIn("All 2 images failed", str(context.exception))

    def test_extract_image_candidates_includes_picture_source(self):
        html = """
        <html><body>
            <picture>
                <source srcset="https://cdn.example.com/image.jpg 1x, https://cdn.example.com/image@2x.jpg 2x">
                <img src="https://cdn.example.com/image.webp">
            </picture>
        </body></html>
        """
        soup = BeautifulSoup(html, "html.parser")
        candidates = logic._extract_image_candidates("https://telegra.ph/demo", soup)
        self.assertEqual(len(candidates), 1)
        self.assertIn("https://cdn.example.com/image.webp", candidates[0])
        self.assertIn("https://cdn.example.com/image.jpg", candidates[0])
        self.assertIn("https://cdn.example.com/image@2x.jpg", candidates[0])

    def test_download_images_uses_picture_source_fallback(self):
        html = """
        <html><head><title>Demo</title></head><body>
            <picture>
                <source srcset="https://cdn.example.com/ok.jpg">
                <img src="https://cdn.example.com/missing.webp">
            </picture>
        </body></html>
        """
        soup = BeautifulSoup(html, "html.parser")
        tasks = {"task-3": {"status": "IN_PROGRESS", "progress": 0, "image_concurrency": 1}}

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            def fake_fetch_image(img_url, timeout, retries, save_path, pool_size, should_cancel=None):
                del timeout, retries, pool_size, should_cancel
                if img_url.endswith(".webp"):
                    raise Exception("Request failed: 404")
                Path(save_path).write_bytes(b"ok-jpg")
                return save_path

            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch("telegram_downloader.services.image_downloader.fetch_image", side_effect=fake_fetch_image):
                        zip_path = logic.download_images(
                            "https://telegra.ph/demo",
                            timeout=5,
                            retries=1,
                            task_id="task-3",
                            image_concurrency=1,
                            tasks_db=tasks,
                        )
                        self.assertTrue(Path(zip_path).is_file())
                        self.assertEqual(tasks["task-3"]["progress"], 1)


if __name__ == "__main__":
    unittest.main()
