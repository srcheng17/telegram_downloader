import os
import tempfile
import unittest
import xml.etree.ElementTree as ET
import zipfile
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

    def test_build_paths_avoids_cbz_name_collision(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            temp_root = Path(temp_dir) / "tmp"
            base_root = Path(temp_dir) / "out"
            temp_root.mkdir(parents=True, exist_ok=True)
            base_root.mkdir(parents=True, exist_ok=True)
            (base_root / "Title.cbz").write_bytes(b"1")
            (base_root / "Title (2).cbz").write_bytes(b"1")

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
        self.assertTrue(zip_path.endswith("Title (3).cbz"))
        self.assertTrue(next_zip_path.endswith("Title (4).cbz"))

    def test_download_images_outputs_cbz_and_writes_comicinfo_from_metadata(self):
        html = "<html><head><title>Ignored</title></head><body><img src='1.jpg'></body></html>"
        soup = BeautifulSoup(html, "html.parser")
        tasks = {
            "task-meta": {
                "status": "IN_PROGRESS",
                "progress": 0,
                "image_concurrency": 1,
                "author": "old-author",
                "series_name": "old-series",
                "comic_name": "old-comic",
                "summary": "old-summary",
                "tags_normalized": "old-tag",
            }
        }
        metadata = {
            "author": "作者A",
            "series_name": "系列S",
            "comic_name": "漫画B",
            "summary": "简介",
            "tags_normalized": "标签1， 标签2, , 标签3",
        }

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            def fake_fetch_image(
                img_url,
                timeout,
                retries,
                save_path,
                pool_size,
                should_cancel=None,
                max_image_bytes=None,
                track_total_bytes=None,
            ):
                del img_url, timeout, retries, pool_size, should_cancel
                del max_image_bytes, track_total_bytes
                Path(save_path).write_bytes(b"img")
                return save_path

            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch("telegram_downloader.services.image_downloader.fetch_image", side_effect=fake_fetch_image):
                        with patch("telegram_downloader.services.image_downloader.time.time", return_value=1700000000):
                            cbz_path = logic.download_images(
                                "https://telegra.ph/demo",
                                timeout=5,
                                retries=1,
                                task_id="task-meta",
                                image_concurrency=1,
                                tasks_db=tasks,
                                metadata=metadata,
                            )

            self.assertTrue(cbz_path.endswith(".cbz"))
            self.assertEqual(Path(cbz_path).name, "作者A_系列S_漫画B_1700000000.cbz")
            with zipfile.ZipFile(cbz_path) as cbz:
                self.assertIn("ComicInfo.xml", cbz.namelist())
                root = ET.fromstring(cbz.read("ComicInfo.xml"))
            self.assertEqual(root.findtext("Writer"), "作者A")
            self.assertEqual(root.findtext("Series"), "系列S")
            self.assertEqual(root.findtext("Title"), "漫画B")
            self.assertEqual(root.findtext("Summary"), "简介")
            self.assertEqual(root.findtext("Tags"), "标签1,标签2,标签3")
            self.assertEqual(root.findtext("Genre"), "标签1,标签2,标签3")

    def test_download_images_uses_task_metadata_fallback_and_placeholder_filename(self):
        html = "<html><head><title>Ignored</title></head><body><img src='1.jpg'></body></html>"
        soup = BeautifulSoup(html, "html.parser")
        tasks = {
            "task-fallback": {
                "status": "IN_PROGRESS",
                "progress": 0,
                "image_concurrency": 1,
                "author": " ",
                "series_name": "任务系列",
                "comic_name": None,
                "summary": "task-summary",
                "tags_raw": "标签甲， 标签乙, , 标签丙",
                "tags_normalized": None,
            }
        }

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            def fake_fetch_image(
                img_url,
                timeout,
                retries,
                save_path,
                pool_size,
                should_cancel=None,
                max_image_bytes=None,
                track_total_bytes=None,
            ):
                del img_url, timeout, retries, pool_size, should_cancel
                del max_image_bytes, track_total_bytes
                Path(save_path).write_bytes(b"img")
                return save_path

            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch("telegram_downloader.services.image_downloader.fetch_image", side_effect=fake_fetch_image):
                        with patch("telegram_downloader.services.image_downloader.time.time", return_value=1700001111):
                            cbz_path = logic.download_images(
                                "https://telegra.ph/demo",
                                timeout=5,
                                retries=1,
                                task_id="task-fallback",
                                image_concurrency=1,
                                tasks_db=tasks,
                            )

            self.assertEqual(Path(cbz_path).name, "未知作者_任务系列_未命名漫画_1700001111.cbz")
            with zipfile.ZipFile(cbz_path) as cbz:
                root = ET.fromstring(cbz.read("ComicInfo.xml"))
            self.assertEqual(root.findtext("Writer"), "未知作者")
            self.assertEqual(root.findtext("Series"), "任务系列")
            self.assertEqual(root.findtext("Title"), "未命名漫画")
            self.assertEqual(root.findtext("Summary"), "task-summary")
            self.assertEqual(root.findtext("Tags"), "标签甲,标签乙,标签丙")
            self.assertEqual(root.findtext("Genre"), "标签甲,标签乙,标签丙")

    def test_download_images_truncates_long_cbz_filename_to_safe_length(self):
        html = "<html><head><title>Ignored</title></head><body><img src='1.jpg'></body></html>"
        soup = BeautifulSoup(html, "html.parser")
        tasks = {"task-long-name": {"status": "IN_PROGRESS", "progress": 0, "image_concurrency": 1}}
        metadata = {
            "author": "A" * 260,
            "comic_name": "B" * 260,
        }

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            def fake_fetch_image(
                img_url,
                timeout,
                retries,
                save_path,
                pool_size,
                should_cancel=None,
                max_image_bytes=None,
                track_total_bytes=None,
            ):
                del img_url, timeout, retries, pool_size, should_cancel
                del max_image_bytes, track_total_bytes
                Path(save_path).write_bytes(b"img")
                return save_path

            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch("telegram_downloader.services.image_downloader.fetch_image", side_effect=fake_fetch_image):
                        with patch("telegram_downloader.services.image_downloader.time.time", return_value=1700002222):
                            cbz_path = logic.download_images(
                                "https://telegra.ph/demo",
                                timeout=5,
                                retries=1,
                                task_id="task-long-name",
                                image_concurrency=1,
                                tasks_db=tasks,
                                metadata=metadata,
                            )

            file_name = Path(cbz_path).name
            self.assertTrue(file_name.endswith(".cbz"))
            self.assertLessEqual(len(file_name.encode("utf-8")), 255)
            self.assertIn("_1700002222.cbz", file_name)
            self.assertTrue(Path(cbz_path).is_file())

    def test_download_images_sanitizes_invalid_xml_characters_for_comicinfo(self):
        html = "<html><head><title>Ignored</title></head><body><img src='1.jpg'></body></html>"
        soup = BeautifulSoup(html, "html.parser")
        tasks = {"task-xml": {"status": "IN_PROGRESS", "progress": 0, "image_concurrency": 1}}
        metadata = {
            "author": "作\x00者",
            "series_name": "系\x00列",
            "comic_name": "漫\x08画",
            "summary": "摘\x0B要\x1Fok",
            "tags_normalized": "标\x00签1， 标\x1F签2",
        }

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            def fake_fetch_image(
                img_url,
                timeout,
                retries,
                save_path,
                pool_size,
                should_cancel=None,
                max_image_bytes=None,
                track_total_bytes=None,
            ):
                del img_url, timeout, retries, pool_size, should_cancel
                del max_image_bytes, track_total_bytes
                Path(save_path).write_bytes(b"img")
                return save_path

            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch("telegram_downloader.services.image_downloader.fetch_image", side_effect=fake_fetch_image):
                        with patch("telegram_downloader.services.image_downloader.time.time", return_value=1700003333):
                            cbz_path = logic.download_images(
                                "https://telegra.ph/demo",
                                timeout=5,
                                retries=1,
                                task_id="task-xml",
                                image_concurrency=1,
                                tasks_db=tasks,
                                metadata=metadata,
                            )

            with zipfile.ZipFile(cbz_path) as cbz:
                root = ET.fromstring(cbz.read("ComicInfo.xml"))
            self.assertEqual(root.findtext("Writer"), "作者")
            self.assertEqual(root.findtext("Series"), "系列")
            self.assertEqual(root.findtext("Summary"), "摘要ok")
            self.assertEqual(root.findtext("Tags"), "标签1,标签2")
            self.assertEqual(root.findtext("Genre"), "标签1,标签2")

    def test_download_images_treats_explicit_empty_metadata_as_user_clear(self):
        html = "<html><head><title>Ignored</title></head><body><img src='1.jpg'></body></html>"
        soup = BeautifulSoup(html, "html.parser")
        tasks = {
            "task-clear": {
                "status": "IN_PROGRESS",
                "progress": 0,
                "image_concurrency": 1,
                "author": "old-author",
                "series_name": "old-series",
                "comic_name": "old-comic",
                "summary": "old-summary",
                "tags_normalized": "old-tag",
                "tags_raw": "old-tag-raw",
            }
        }
        metadata = {
            "author": "",
            "series_name": "",
            "summary": " ",
            "tags_normalized": "",
        }

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            def fake_fetch_image(
                img_url,
                timeout,
                retries,
                save_path,
                pool_size,
                should_cancel=None,
                max_image_bytes=None,
                track_total_bytes=None,
            ):
                del img_url, timeout, retries, pool_size, should_cancel
                del max_image_bytes, track_total_bytes
                Path(save_path).write_bytes(b"img")
                return save_path

            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch("telegram_downloader.services.image_downloader.fetch_image", side_effect=fake_fetch_image):
                        with patch("telegram_downloader.services.image_downloader.time.time", return_value=1700004444):
                            cbz_path = logic.download_images(
                                "https://telegra.ph/demo",
                                timeout=5,
                                retries=1,
                                task_id="task-clear",
                                image_concurrency=1,
                                tasks_db=tasks,
                                metadata=metadata,
                            )

            self.assertEqual(Path(cbz_path).name, "未知作者_old-comic_1700004444.cbz")
            with zipfile.ZipFile(cbz_path) as cbz:
                root = ET.fromstring(cbz.read("ComicInfo.xml"))
            self.assertEqual(root.findtext("Writer"), "未知作者")
            self.assertIsNone(root.find("Series").text)
            self.assertIsNone(root.find("Summary").text)
            self.assertIsNone(root.find("Tags").text)
            self.assertIsNone(root.find("Genre").text)

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
            def fake_fetch_image(
                img_url,
                timeout,
                retries,
                save_path,
                pool_size,
                should_cancel=None,
                max_image_bytes=None,
                track_total_bytes=None,
            ):
                del timeout, retries, pool_size, should_cancel
                del max_image_bytes, track_total_bytes
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
            def fake_fetch_image(
                img_url,
                timeout,
                retries,
                save_path,
                pool_size,
                should_cancel=None,
                max_image_bytes=None,
                track_total_bytes=None,
            ):
                del timeout, retries, pool_size, should_cancel
                del max_image_bytes, track_total_bytes
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

    def test_download_images_fails_with_clear_error_when_image_count_limit_exceeded(self):
        html = """
        <html><head><title>Demo</title></head><body>
            <img src='1.jpg'><img src='2.jpg'><img src='3.jpg'>
        </body></html>
        """
        soup = BeautifulSoup(html, "html.parser")
        tasks = {"task-limit": {"status": "IN_PROGRESS", "progress": 0, "image_concurrency": 1}}

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch("telegram_downloader.services.image_downloader.MAX_IMAGES_PER_TASK", 2):
                        with self.assertRaises(logic.DownloadLimitExceededError) as context:
                            logic.download_images(
                                "https://telegra.ph/demo",
                                timeout=5,
                                retries=1,
                                task_id="task-limit",
                                image_concurrency=1,
                                tasks_db=tasks,
                            )

        self.assertIn("Image count limit exceeded", str(context.exception))
        self.assertEqual(tasks["task-limit"]["status"], "FAILED")
        self.assertIn("max allowed is 2", tasks["task-limit"]["error"])

    def test_download_images_fails_with_clear_error_when_total_size_limit_exceeded(self):
        html = "<html><head><title>Demo</title></head><body><img src='1.jpg'></body></html>"
        soup = BeautifulSoup(html, "html.parser")
        tasks = {"task-total-limit": {"status": "IN_PROGRESS", "progress": 0, "image_concurrency": 1}}

        with tempfile.TemporaryDirectory() as base_dir, tempfile.TemporaryDirectory() as temp_dir:
            with patch.dict(os.environ, {"DOWNLOAD_PATH": base_dir, "TEMP_PATH": temp_dir}, clear=False):
                with patch("telegram_downloader.services.image_downloader._fetch_page_soup", return_value=soup):
                    with patch(
                        "telegram_downloader.services.image_downloader._get_thread_session",
                        return_value=_AlwaysSuccessSession(),
                    ):
                        with patch("telegram_downloader.services.image_downloader.MAX_TOTAL_DOWNLOAD_BYTES", 5):
                            with patch("telegram_downloader.services.image_downloader.MAX_IMAGE_BYTES", 10_000):
                                with self.assertRaises(Exception) as context:
                                    logic.download_images(
                                        "https://telegra.ph/demo",
                                        timeout=5,
                                        retries=0,
                                        task_id="task-total-limit",
                                        image_concurrency=1,
                                        tasks_db=tasks,
                                    )

        self.assertIn("Total download size limit exceeded", str(context.exception))
        self.assertEqual(tasks["task-total-limit"]["status"], "FAILED")
        self.assertIn("Total download size limit exceeded", tasks["task-total-limit"]["error"])


if __name__ == "__main__":
    unittest.main()
