import os
import re
import requests
from bs4 import BeautifulSoup
from urllib.parse import urljoin
import shutil
import time
from concurrent.futures import ThreadPoolExecutor, as_completed

def zip_folder(source_folder, dest_zip_path):
    """Zips a folder to a destination and then deletes the source folder."""
    if not os.path.isdir(source_folder):
        return
    try:
        # Ensure the destination directory exists
        dest_dir = os.path.dirname(dest_zip_path)
        if not os.path.exists(dest_dir):
            os.makedirs(dest_dir)

        # If a file with the same name exists, remove it
        if os.path.exists(dest_zip_path):
            os.remove(dest_zip_path)
            
        shutil.make_archive(os.path.splitext(dest_zip_path)[0], 'zip', source_folder)
        shutil.rmtree(source_folder)
    except Exception as e:
        raise Exception(f"Error during zip or delete process: {e}")

def fetch_image(session, url, timeout, retries, save_path):
    """Fetches a single image with retries and saves it directly to disk."""
    for attempt in range(retries):
        try:
            with session.get(url, timeout=timeout, stream=True) as response:
                response.raise_for_status()
                with open(save_path, 'wb') as f:
                    for chunk in response.iter_content(chunk_size=8192):
                        f.write(chunk)
                return save_path
        except (requests.exceptions.Timeout, requests.exceptions.ConnectionError, requests.exceptions.ChunkedEncodingError):
            if attempt + 1 == retries:
                raise
            time.sleep(2)  # Sleep a bit longer for connection errors
        except requests.exceptions.RequestException as e:
            # If the file was partially created, clean it up
            if os.path.exists(save_path):
                os.remove(save_path)
            raise Exception(f"Request failed for {url}: {e}")
    raise Exception(f"Failed to download {url} after {retries} retries.")

def download_images(url, timeout=10, retries=5, task_id=None, tasks_db=None):
    base_folder = os.environ.get('DOWNLOAD_PATH', 'downloaded_images')
    temp_folder = os.environ.get('TEMP_PATH', 'temp_downloads')
    """
    Concurrently downloads all images from a Telegraph page into a temporary directory,
    zips them, and then moves the zip to the final destination.
    """
    if not os.path.exists(temp_folder):
        os.makedirs(temp_folder)
    if not os.path.exists(base_folder):
        os.makedirs(base_folder)

    folder_name = temp_folder
    try:
        with requests.Session() as s:
            s.headers.update({'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36'})
            page_content = s.get(url, timeout=timeout).text
            soup = BeautifulSoup(page_content, 'html.parser')
            
            page_title = soup.title.string if soup.title else "Untitled"
            
            # Sanitize the title to create a safe filename
            # Example: "AhrStudio-RobinAHR-July-2025-08-02" -> "[AhrStudio] RobinAHR July 2025"
            sanitized_title = re.sub(r'-\d{2}-\d{2}$', '', page_title)  # Remove date suffix
            sanitized_title = sanitized_title.replace('-', ' ') # Replace hyphens with spaces
            
            # Extract the author part if it exists
            author_match = re.match(r'^(\w+)', sanitized_title)
            if author_match:
                author = author_match.group(1)
                rest_of_title = sanitized_title[len(author):].strip()
                safe_title = f"[{author}] {rest_of_title}"
            else:
                safe_title = sanitized_title
            temp_folder_path = os.path.join(temp_folder, safe_title)
            final_zip_path = os.path.join(base_folder, f"{safe_title}.zip")

            if not os.path.exists(temp_folder_path):
                os.makedirs(temp_folder_path)

            img_tags = soup.find_all('img')
            if not img_tags:
                raise ValueError("No images found on the page.")

            image_urls = [urljoin(url, img.get('src')) for img in img_tags if img.get('src')]
            if task_id and tasks_db:
                tasks_db[task_id]['total_images'] = len(image_urls)
            
            # Use a ThreadPoolExecutor to download images concurrently
            # The concurrency is handled by the main app's executor
            downloaded_images = [None] * len(image_urls)
            
            # The app's executor controls how many *tasks* run, not images per task.
            concurrency = tasks_db.get(task_id, {}).get('concurrency', 5) if task_id and tasks_db else 5
            with ThreadPoolExecutor(max_workers=concurrency) as executor:
                future_to_path = {}
                for i, img_url in enumerate(image_urls):
                    file_extension = os.path.splitext(img_url)[1] or '.jpg'
                    if len(file_extension) > 5: file_extension = '.jpg'
                    image_path = os.path.join(temp_folder_path, f"{i + 1}{file_extension}")
                    future_to_path[executor.submit(fetch_image, s, img_url, timeout, retries, image_path)] = image_path

                for future in as_completed(future_to_path):
                    try:
                        future.result()
                        if task_id and tasks_db:
                            tasks_db[task_id]['progress'] += 1
                    except Exception as exc:
                        # The exception already includes details, just re-raise
                        raise exc

    except Exception as e:
        if task_id and tasks_db:
            tasks_db[task_id]['error'] = str(e)
            tasks_db[task_id]['status'] = 'FAILED'
        raise

    try:
        zip_folder(temp_folder_path, final_zip_path)
    except Exception as e:
        if task_id and tasks_db:
            tasks_db[task_id]['error'] = f"Download succeeded, but zip failed: {e}"
            tasks_db[task_id]['status'] = 'FAILED'
        raise
