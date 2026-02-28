import os
import re
import shutil
import threading
import time
from concurrent.futures import FIRST_COMPLETED, ThreadPoolExecutor, wait
from urllib.parse import urljoin, urlparse

import requests
from bs4 import BeautifulSoup
from requests.adapters import HTTPAdapter

REQUEST_HEADERS = {
    "User-Agent": (
        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 "
        "(KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
    )
}
INVALID_FILENAME_CHARS = re.compile(r'[<>:"/\\|?*\x00-\x1F]')
VALID_EXTENSION_PATTERN = re.compile(r"\.[a-z0-9]{1,5}")
MAX_TITLE_LENGTH = 120
RETRY_BACKOFF_BASE_SECONDS = 0.5
RETRY_BACKOFF_MAX_SECONDS = 5
TRANSIENT_HTTP_STATUS_CODES = {408, 425, 429, 500, 502, 503, 504}
_SESSION_LOCAL = threading.local()
_ZIP_PATH_LOCK = threading.Lock()


class DownloadCancelledError(Exception):
    """Raised when a user requests cancellation for an in-flight task."""


class PartialDownloadError(Exception):
    """Raised when at least one image succeeds but some still fail."""


def _set_task_fields(tasks_db, tasks_lock, task_id, **fields):
    if not (task_id and tasks_db):
        return
    if hasattr(tasks_db, "update_task"):
        tasks_db.update_task(task_id, **fields)
        return
    if tasks_lock:
        with tasks_lock:
            task = tasks_db.get(task_id)
            if task:
                task.update(fields)
        return
    task = tasks_db.get(task_id)
    if task:
        task.update(fields)


def _increment_task_progress(tasks_db, tasks_lock, task_id, step=1):
    if not (task_id and tasks_db):
        return
    if hasattr(tasks_db, "increment_progress"):
        tasks_db.increment_progress(task_id, step=step)
        return
    if tasks_lock:
        with tasks_lock:
            task = tasks_db.get(task_id)
            if task:
                task["progress"] += step
        return
    task = tasks_db.get(task_id)
    if task:
        task["progress"] += step


def _get_task_field(tasks_db, tasks_lock, task_id, field, default):
    if not (task_id and tasks_db):
        return default
    if hasattr(tasks_db, "get_field"):
        return tasks_db.get_field(task_id, field, default)
    if tasks_lock:
        with tasks_lock:
            task = tasks_db.get(task_id, {})
            return task.get(field, default)
    task = tasks_db.get(task_id, {})
    return task.get(field, default)


def sanitize_title(page_title):
    """Convert remote title text into a safe cross-platform filename."""
    title = page_title or "Untitled"

    # Keep existing naming intent while removing the trailing month-day suffix.
    normalized_title = re.sub(r"-\d{2}-\d{2}$", "", title).replace("-", " ")
    author_match = re.match(r"^(\w+)", normalized_title)
    if author_match:
        author = author_match.group(1)
        rest_of_title = normalized_title[len(author) :].strip()
        safe_title = f"[{author}] {rest_of_title}".strip()
    else:
        safe_title = normalized_title.strip()

    safe_title = INVALID_FILENAME_CHARS.sub(" ", safe_title)
    safe_title = re.sub(r"\s+", " ", safe_title).strip(" .")
    if safe_title in {"", ".", ".."}:
        safe_title = "Untitled"
    return safe_title[:MAX_TITLE_LENGTH]


def _new_session(pool_size):
    session = requests.Session()
    adapter = HTTPAdapter(pool_connections=pool_size, pool_maxsize=pool_size)
    session.mount("http://", adapter)
    session.mount("https://", adapter)
    session.headers.update(REQUEST_HEADERS)
    return session


def _get_thread_session(pool_size):
    session = getattr(_SESSION_LOCAL, "session", None)
    session_pool_size = getattr(_SESSION_LOCAL, "pool_size", None)
    if session is not None and session_pool_size == pool_size:
        return session
    if session is not None:
        session.close()
    session = _new_session(pool_size)
    _SESSION_LOCAL.session = session
    _SESSION_LOCAL.pool_size = pool_size
    return session


def _retry_backoff_seconds(attempt_index):
    return min(RETRY_BACKOFF_BASE_SECONDS * (2**attempt_index), RETRY_BACKOFF_MAX_SECONDS)


def _reserve_zip_output_path(base_folder, safe_title):
    flags = os.O_CREAT | os.O_EXCL | os.O_WRONLY
    if hasattr(os, "O_BINARY"):
        flags |= os.O_BINARY

    with _ZIP_PATH_LOCK:
        suffix = 1
        while True:
            name = f"{safe_title}.zip" if suffix == 1 else f"{safe_title} ({suffix}).zip"
            candidate = os.path.join(base_folder, name)
            try:
                fd = os.open(candidate, flags)
                os.close(fd)
                return candidate
            except FileExistsError:
                suffix += 1
                continue


def _build_paths(temp_folder, base_folder, safe_title, task_id):
    task_suffix = (task_id or str(int(time.time() * 1000)))[:8]
    temp_folder_path = os.path.join(temp_folder, f"{safe_title}-{task_suffix}")
    final_zip_path = _reserve_zip_output_path(base_folder, safe_title)
    return temp_folder_path, final_zip_path


def _cleanup_file(path):
    try:
        os.remove(path)
    except FileNotFoundError:
        return


def _cleanup_reserved_zip(path):
    if not path or not os.path.isfile(path):
        return
    try:
        if os.path.getsize(path) == 0:
            os.remove(path)
    except OSError:
        return


def _is_retryable_http_status(status_code):
    return status_code in TRANSIENT_HTTP_STATUS_CODES


def _is_task_cancelled(tasks_db, tasks_lock, task_id):
    if not task_id:
        return False
    status = _get_task_field(tasks_db, tasks_lock, task_id, "status", "")
    return status in {"CANCEL_REQUESTED", "CANCELED"}


def _raise_if_task_cancelled(tasks_db, tasks_lock, task_id):
    if _is_task_cancelled(tasks_db, tasks_lock, task_id):
        raise DownloadCancelledError("Cancellation requested by user.")


def _fetch_page_soup(url, timeout, retries):
    attempts = max(1, retries + 1)
    with _new_session(pool_size=1) as metadata_session:
        for attempt in range(attempts):
            try:
                response = metadata_session.get(url, timeout=timeout)
                response.raise_for_status()
                return BeautifulSoup(response.text, "html.parser")
            except (
                requests.exceptions.Timeout,
                requests.exceptions.ConnectionError,
                requests.exceptions.ChunkedEncodingError,
            ) as e:
                if attempt + 1 == attempts:
                    raise Exception(f"Request failed for {url}: {e}")
                time.sleep(_retry_backoff_seconds(attempt))
            except requests.exceptions.HTTPError as e:
                status_code = e.response.status_code if e.response is not None else None
                if _is_retryable_http_status(status_code) and attempt + 1 < attempts:
                    time.sleep(_retry_backoff_seconds(attempt))
                    continue
                raise Exception(f"Request failed for {url}: {e}")
            except requests.exceptions.RequestException as e:
                raise Exception(f"Request failed for {url}: {e}")


def _iter_srcset_urls(srcset_value):
    if not srcset_value:
        return []
    results = []
    for item in srcset_value.split(","):
        candidate = (item or "").strip().split(" ")[0].strip()
        if candidate:
            results.append(candidate)
    return results


def _extract_image_candidates(page_url, soup):
    candidates_per_image = []
    for img in soup.find_all("img"):
        raw_candidates = []
        for attr in ("src", "data-src", "data-original", "data-lazy-src", "data-url"):
            value = img.get(attr)
            if value:
                raw_candidates.append(value)
        raw_candidates.extend(_iter_srcset_urls(img.get("srcset")))
        raw_candidates.extend(_iter_srcset_urls(img.get("data-srcset")))

        # Some pages use <picture><source srcset=...><img ...></picture>.
        # Include source candidates to avoid missing non-default formats.
        picture = img.find_parent("picture")
        if picture is not None:
            for source in picture.find_all("source"):
                raw_candidates.extend(_iter_srcset_urls(source.get("srcset")))
                raw_candidates.extend(_iter_srcset_urls(source.get("data-srcset")))
                source_src = source.get("src")
                if source_src:
                    raw_candidates.append(source_src)

        normalized_candidates = []
        seen = set()
        for raw_url in raw_candidates:
            absolute_url = urljoin(page_url, raw_url.strip())
            parsed = urlparse(absolute_url)
            if parsed.scheme not in {"http", "https"}:
                continue
            if absolute_url in seen:
                continue
            seen.add(absolute_url)
            normalized_candidates.append(absolute_url)

        if normalized_candidates:
            candidates_per_image.append(normalized_candidates)
    return candidates_per_image


def zip_folder(source_folder, dest_zip_path):
    """Zips a folder to a destination and then deletes the source folder."""
    if not os.path.isdir(source_folder):
        return
    temp_archive_path = None
    try:
        dest_dir = os.path.dirname(dest_zip_path)
        os.makedirs(dest_dir, exist_ok=True)
        temp_archive_base = f"{os.path.splitext(dest_zip_path)[0]}.__tmp__"
        temp_archive_path = f"{temp_archive_base}.zip"
        _cleanup_file(temp_archive_path)
        generated_archive_path = shutil.make_archive(temp_archive_base, "zip", source_folder)
        os.replace(generated_archive_path, dest_zip_path)
        shutil.rmtree(source_folder)
    except Exception as e:
        _cleanup_file(temp_archive_path)
        raise Exception(f"Error during zip or delete process: {e}")


def fetch_image(url, timeout, retries, save_path, pool_size, should_cancel=None):
    """Fetches a single image with retries and saves it directly to disk."""
    attempts = max(1, retries + 1)
    session = _get_thread_session(pool_size)
    for attempt in range(attempts):
        if should_cancel and should_cancel():
            _cleanup_file(save_path)
            raise DownloadCancelledError("Cancellation requested by user.")
        try:
            with session.get(url, timeout=timeout, stream=True) as response:
                response.raise_for_status()
                with open(save_path, "wb") as file_obj:
                    for chunk in response.iter_content(chunk_size=8192):
                        if should_cancel and should_cancel():
                            raise DownloadCancelledError("Cancellation requested by user.")
                        if chunk:
                            file_obj.write(chunk)
                return save_path
        except DownloadCancelledError:
            _cleanup_file(save_path)
            raise
        except (
            requests.exceptions.Timeout,
            requests.exceptions.ConnectionError,
            requests.exceptions.ChunkedEncodingError,
        ) as e:
            _cleanup_file(save_path)
            if attempt + 1 == attempts:
                raise Exception(f"Request failed for {url}: {e}")
            time.sleep(_retry_backoff_seconds(attempt))
        except requests.exceptions.HTTPError as e:
            _cleanup_file(save_path)
            status_code = e.response.status_code if e.response is not None else None
            if _is_retryable_http_status(status_code) and attempt + 1 < attempts:
                time.sleep(_retry_backoff_seconds(attempt))
                continue
            raise Exception(f"Request failed for {url}: {e}")
        except requests.exceptions.RequestException as e:
            _cleanup_file(save_path)
            raise Exception(f"Request failed for {url}: {e}")
    raise Exception(f"Failed to download {url} after {attempts} retries.")


def fetch_image_with_fallback(candidate_urls, timeout, retries, save_path, pool_size, should_cancel=None):
    if not candidate_urls:
        raise Exception("No candidate URLs available for this image.")
    last_error = None
    for candidate_url in candidate_urls:
        try:
            return fetch_image(
                candidate_url,
                timeout=timeout,
                retries=retries,
                save_path=save_path,
                pool_size=pool_size,
                should_cancel=should_cancel,
            )
        except DownloadCancelledError:
            raise
        except Exception as exc:
            last_error = exc
    raise Exception(f"All candidate URLs failed. Last error: {last_error}")


def download_images(
    url,
    timeout=10,
    retries=5,
    task_id=None,
    image_concurrency=None,
    tasks_db=None,
    tasks_lock=None,
):
    """
    Concurrently downloads all images from a Telegraph page into a temporary directory,
    zips them, and then moves the zip to the final destination.
    """
    base_folder = os.environ.get("DOWNLOAD_PATH", "downloaded_images")
    temp_folder = os.environ.get("TEMP_PATH", "temp_downloads")

    os.makedirs(temp_folder, exist_ok=True)
    os.makedirs(base_folder, exist_ok=True)

    temp_folder_path = None
    final_zip_path = None
    try:
        _raise_if_task_cancelled(tasks_db, tasks_lock, task_id)
        soup = _fetch_page_soup(url, timeout=timeout, retries=retries)
        _raise_if_task_cancelled(tasks_db, tasks_lock, task_id)

        page_title = soup.title.string.strip() if soup.title and soup.title.string else "Untitled"
        safe_title = sanitize_title(page_title)
        temp_folder_path, final_zip_path = _build_paths(temp_folder, base_folder, safe_title, task_id)
        os.makedirs(temp_folder_path, exist_ok=True)

        image_candidates = _extract_image_candidates(url, soup)
        if not image_candidates:
            raise ValueError("No images found on the page.")

        _set_task_fields(tasks_db, tasks_lock, task_id, total_images=len(image_candidates))

        if image_concurrency is None:
            concurrency = _get_task_field(tasks_db, tasks_lock, task_id, "image_concurrency", 5)
        else:
            concurrency = image_concurrency
        try:
            concurrency = max(1, int(concurrency))
        except (TypeError, ValueError):
            concurrency = 5

        executor = ThreadPoolExecutor(max_workers=concurrency)
        try:
            futures = set()
            successful_images = 0
            failed_errors = []
            failed_entries = []
            future_to_job = {}
            for i, candidate_urls in enumerate(image_candidates):
                _raise_if_task_cancelled(tasks_db, tasks_lock, task_id)
                parsed_path = urlparse(candidate_urls[0]).path
                file_extension = os.path.splitext(parsed_path)[1].lower() or ".jpg"
                if not VALID_EXTENSION_PATTERN.fullmatch(file_extension):
                    file_extension = ".jpg"
                image_path = os.path.join(temp_folder_path, f"{i + 1}{file_extension}")
                future = executor.submit(
                    fetch_image_with_fallback,
                    candidate_urls,
                    timeout=timeout,
                    retries=retries,
                    save_path=image_path,
                    pool_size=concurrency,
                    should_cancel=lambda: _is_task_cancelled(tasks_db, tasks_lock, task_id),
                )
                futures.add(future)
                future_to_job[future] = (candidate_urls, image_path)

            while futures:
                _raise_if_task_cancelled(tasks_db, tasks_lock, task_id)
                done, pending = wait(futures, timeout=0.5, return_when=FIRST_COMPLETED)
                if not done:
                    futures = pending
                    continue
                for future in done:
                    _raise_if_task_cancelled(tasks_db, tasks_lock, task_id)
                    try:
                        future.result()
                        _increment_task_progress(tasks_db, tasks_lock, task_id)
                        successful_images += 1
                    except DownloadCancelledError:
                        raise
                    except Exception as exc:
                        candidate_urls, image_path = future_to_job.get(future, ([], ""))
                        failed_entries.append((candidate_urls, image_path))
                        failed_errors.append(str(exc))
                futures = pending

            for candidate_urls, image_path in failed_entries:
                _raise_if_task_cancelled(tasks_db, tasks_lock, task_id)
                try:
                    fetch_image_with_fallback(
                        candidate_urls,
                        timeout=max(timeout, 10),
                        retries=max(retries, 3),
                        save_path=image_path,
                        pool_size=1,
                        should_cancel=lambda: _is_task_cancelled(tasks_db, tasks_lock, task_id),
                    )
                    _increment_task_progress(tasks_db, tasks_lock, task_id)
                    successful_images += 1
                except DownloadCancelledError:
                    raise
                except Exception as exc:
                    failed_errors.append(str(exc))

            total_images = len(image_candidates)
            failed_count = max(0, total_images - successful_images)
            if successful_images == 0:
                sample_error = failed_errors[0] if failed_errors else "No images were saved."
                raise Exception(
                    f"All {total_images} images failed to download. Example error: {sample_error}"
                )

            if failed_count > 0:
                sample_error = failed_errors[0] if failed_errors else "Unknown error."
                raise PartialDownloadError(
                    (
                        f"Partial download failed: {successful_images}/{total_images} images downloaded. "
                        f"{failed_count} failed. Example error: {sample_error}"
                    )
                )
            _set_task_fields(tasks_db, tasks_lock, task_id, error=None)
        finally:
            executor.shutdown(wait=True, cancel_futures=True)

    except DownloadCancelledError:
        _set_task_fields(tasks_db, tasks_lock, task_id, error="Cancelled by user.", status="CANCELED")
        if temp_folder_path and os.path.isdir(temp_folder_path):
            shutil.rmtree(temp_folder_path, ignore_errors=True)
        _cleanup_reserved_zip(final_zip_path)
        raise

    except Exception as e:
        _set_task_fields(tasks_db, tasks_lock, task_id, error=str(e), status="FAILED")
        if temp_folder_path and os.path.isdir(temp_folder_path):
            shutil.rmtree(temp_folder_path, ignore_errors=True)
        _cleanup_reserved_zip(final_zip_path)
        raise

    try:
        zip_folder(temp_folder_path, final_zip_path)
    except Exception as e:
        _set_task_fields(
            tasks_db,
            tasks_lock,
            task_id,
            error=f"Download succeeded, but zip failed: {e}",
            status="FAILED",
        )
        _cleanup_reserved_zip(final_zip_path)
        raise
    return final_zip_path
