import contextlib
import os
import socket
import subprocess
import sys
import threading
import time
import zipfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from xml.etree import ElementTree

import httpx
import pytest
from playwright.sync_api import expect, sync_playwright

from tests.test_api import PNG, archive_bytes, wait_job

pytestmark = pytest.mark.e2e
ARTIFACTS = Path("test-results")


@pytest.fixture(scope="module")
def live_app(tmp_path_factory):
    root = tmp_path_factory.mktemp("browser")
    release = threading.Event()

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def do_GET(self):
            if self.path == "/missing.png":
                self.send_error(404)
                return
            if self.path.endswith(".png"):
                if self.path == "/slow.png":
                    release.wait(20)
                body, content_type = PNG, "image/png"
            else:
                second = "/missing.png" if self.path == "/partial" else (
                    "/slow.png" if self.path == "/slow" else "/2.png")
                body = (
                    '<meta property="og:title" content="Offline comic">'
                    '<link rel="canonical" href="https://telegra.ph/test">'
                    '<article><figure><img src="/1.png"></figure>'
                    f'<figure><img src="{second}"></figure></article>'
                ).encode()
                content_type = "text/html"
            self.send_response(200)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            with contextlib.suppress(BrokenPipeError, ConnectionResetError):
                self.wfile.write(body)

    source = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=source.serve_forever, daemon=True)
    thread.start()
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    (root / "komga").mkdir()
    env = {**os.environ, "DATA_DIR": str(root / "data"), "KOMGA_DIR": str(root / "komga"),
           "GALLERY_FIXTURE_ROOT": f"http://127.0.0.1:{source.server_port}"}
    with (root / "server.log").open("w") as log:
        process = subprocess.Popen(
            [sys.executable, "-m", "uvicorn", "tests.browser_app:app", "--host", "127.0.0.1",
             "--port", str(port), "--no-access-log"], env=env, stdout=log, stderr=log,
        )
        url = f"http://127.0.0.1:{port}"
        try:
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                try:
                    if httpx.get(url + "/healthz").status_code == 200:
                        break
                except httpx.HTTPError:
                    pass
                if process.poll() is not None:
                    pytest.fail((root / "server.log").read_text())
                time.sleep(0.05)
            else:
                pytest.fail("isolated application did not start")
            yield url, root
        finally:
            release.set()
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
            source.shutdown()
            source.server_close()
            thread.join(timeout=3)


@pytest.fixture
def page(live_app):
    with sync_playwright() as playwright:
        browser = playwright.chromium.launch(headless=True)
        context = browser.new_context(viewport={"width": 1440, "height": 1080}, reduced_motion="reduce")
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        yield page
        assert errors == []
        context.close()
        browser.close()


def test_desktop_upload_automatically_delivers_to_komga(page, live_app):
    url, root = live_app
    page.goto(url)
    page.wait_for_load_state("networkidle")
    expect(page.get_by_test_id("job-list")).to_have_count(0)
    expect(page.get_by_test_id("nav-home")).to_have_attribute("aria-current", "page")
    ARTIFACTS.mkdir(exist_ok=True)
    page.screenshot(path=str(ARTIFACTS / "desktop-home.png"), full_page=True)
    page.get_by_test_id("mode-upload").check()
    page.get_by_test_id("source-file").set_input_files({"name": "comic.zip", "mimeType": "application/zip",
                                                       "buffer": archive_bytes()})
    page.get_by_test_id("comic-name").fill("海边的图页<&>")
    page.get_by_test_id("author").fill("测试作者")
    page.get_by_test_id("series-name").fill("海岸手记")
    page.get_by_test_id("series-number").fill("3")
    page.get_by_test_id("summary").fill("一段简介\n另一段")
    page.get_by_test_id("tags").fill("海边,日常")
    page.get_by_test_id("genres").fill("生活")
    page.get_by_test_id("submit-job").click()
    expect(page.get_by_test_id("view-submitted-job")).to_be_visible()
    expect(page.get_by_test_id("job-list")).to_have_count(0)
    page.get_by_test_id("view-submitted-job").click()
    row = page.get_by_test_id("job-row").filter(has=page.get_by_text("海边的图页<&>", exact=True))
    expect(row.locator("[data-job-status]")).to_have_text("已入库", timeout=15000)
    expect(page.get_by_test_id("download-job")).to_have_count(0)
    expect(page.get_by_test_id("komga-job")).to_have_count(0)
    targets = list((root / "komga" / "海岸手记").glob("*.cbz"))
    assert len(targets) == 1
    with zipfile.ZipFile(targets[0]) as archive:
        xml = ElementTree.fromstring(archive.read("ComicInfo.xml"))
        assert xml.findtext("Title") == "海边的图页<&>"
        assert xml.findtext("Writer") == "测试作者"
        assert xml.findtext("Number") == "3"
        assert archive.read("00001.png") == PNG
        assert archive.read("00002.png") == PNG + b"second"
    page.evaluate("window.scrollTo(0, 0)")
    page.screenshot(path=str(ARTIFACTS / "desktop-completed.png"), full_page=True)


def test_mobile_layout_settings_and_error_recovery(page, live_app):
    url, root = live_app
    page.set_viewport_size({"width": 390, "height": 844})
    page.goto(url)
    page.wait_for_load_state("networkidle")
    assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")
    page.get_by_test_id("source-url").fill("https://example.com/not-telegraph")
    page.get_by_test_id("submit-job").click()
    expect(page.get_by_test_id("submit-feedback")).to_contain_text("Telegraph")
    expect(page.get_by_test_id("submit-job")).to_be_enabled()
    page.screenshot(path=str(ARTIFACTS / "mobile-home.png"), full_page=True)
    page.get_by_test_id("nav-settings").click()
    page.get_by_test_id("komga-url").fill("http://komga.example:25600")
    page.get_by_test_id("library-path").fill("/missing/library")
    page.get_by_test_id("save-settings").click()
    expect(page.get_by_test_id("settings-feedback")).to_contain_text("绝对目录")
    expect(page.get_by_test_id("save-settings")).to_be_enabled()
    page.get_by_test_id("library-path").fill(str(root / "komga"))
    page.get_by_test_id("save-settings").click()
    expect(page.get_by_test_id("settings-feedback")).to_contain_text("已保存")
    page.reload()
    expect(page.get_by_test_id("komga-url")).to_have_value("http://komga.example:25600")
    expect(page.get_by_test_id("library-path")).to_have_value(str(root / "komga"))
    page.get_by_test_id("nav-tasks").click()
    expect(page.get_by_test_id("nav-tasks")).to_have_attribute("aria-current", "page")
    expect(page.get_by_test_id("open-komga")).to_have_attribute("href", "http://komga.example:25600")
    assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")


def test_real_url_import_failure_retry_and_cancel(page, live_app):
    url, root = live_app

    def submit(name, path):
        page.goto(url)
        page.get_by_test_id("source-url").fill(f"https://telegra.ph/{path}")
        page.get_by_test_id("comic-name").fill(name)
        page.get_by_test_id("submit-job").click()
        page.get_by_test_id("view-submitted-job").click()
        return page.get_by_test_id("job-row").filter(has=page.get_by_text(name, exact=True))

    row = submit("离线图集", "book")
    expect(row.locator("[data-job-status]")).to_have_text("已入库", timeout=15000)
    expect(row.locator("[data-job-facts]")).to_contain_text("2 页")
    assert any("离线图集" in path.name for path in (root / "komga" / "tankobon").glob("*.cbz"))
    failed = submit("缺页图集", "partial")
    expect(failed.locator("[data-job-status]")).to_have_text("处理失败", timeout=15000)
    failed.get_by_test_id("retry-job").click()
    expect(failed.locator("[data-job-feedback]")).to_contain_text("已重新加入")
    expect(failed.locator("[data-job-status]")).to_have_text("处理失败", timeout=15000)
    cancelled = submit("取消图集", "slow")
    expect(cancelled.locator("[data-job-status]")).to_have_text("处理中", timeout=15000)
    cancelled.get_by_test_id("cancel-job").click()
    expect(cancelled.locator("[data-job-status]")).to_have_text("已取消", timeout=10000)
    assert not any("缺页图集" in p.name or "取消图集" in p.name for p in (root / "komga").rglob("*.cbz"))


def test_choose_between_two_libraries_for_upload_and_url(page, live_app):
    url, root = live_app
    japanese, chinese = root / "libraries" / "japanese", root / "libraries" / "chinese"
    japanese.mkdir(parents=True)
    chinese.mkdir()
    page.goto(url + "/settings")
    first = page.get_by_test_id("library-row").first
    first.get_by_test_id("library-name").fill("日漫")
    first.get_by_test_id("library-path").fill(str(japanese))
    page.get_by_test_id("add-library").click()
    second = page.get_by_test_id("library-row").nth(1)
    second.get_by_test_id("library-name").fill("国漫")
    second.get_by_test_id("library-path").fill(str(chinese))
    page.get_by_test_id("save-settings").click()
    expect(page.get_by_test_id("settings-feedback")).to_contain_text("已保存")
    saved = httpx.get(url + "/api/settings").json()["libraries"]
    assert [(library["name"], library["path"]) for library in saved] == [
        ("日漫", str(japanese)), ("国漫", str(chinese)),
    ]
    assert len({library["id"] for library in saved}) == 2
    assert all(library["id"] for library in saved)
    page.reload()
    expect(page.get_by_test_id("library-name").nth(1)).to_have_value("国漫")
    page.set_viewport_size({"width": 390, "height": 844})
    assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")
    ARTIFACTS.mkdir(exist_ok=True)
    page.screenshot(path=str(ARTIFACTS / "mobile-libraries.png"), full_page=True)

    submissions, list_requests = [], []

    def observe(request):
        if request.method == "POST" and request.url.endswith("/api/jobs/upload"):
            submissions.append(request)
        if request.method == "GET" and request.url.endswith("/api/jobs"):
            list_requests.append(request)

    page.on("request", observe)
    page.goto(url)
    page.wait_for_load_state("networkidle")
    selection = page.get_by_test_id("library-select")
    expect(selection).to_have_value("")
    page.get_by_test_id("mode-upload").check()
    page.get_by_test_id("source-file").set_input_files({
        "name": "comic.zip", "mimeType": "application/zip", "buffer": archive_bytes(),
    })
    page.get_by_test_id("comic-name").fill("国漫分流验证")
    page.get_by_test_id("series-name").fill("同名系列")
    page.get_by_test_id("submit-job").click()
    assert selection.evaluate("element => element.validity.valueMissing")
    assert submissions == []
    selection.select_option(label="国漫")
    page.get_by_test_id("submit-job").click()
    expect(page.get_by_test_id("view-submitted-job")).to_be_visible()
    assert len(submissions) == 1
    assert list_requests == []
    assert page.evaluate("document.documentElement.scrollWidth <= window.innerWidth")
    page.screenshot(path=str(ARTIFACTS / "mobile-library-selection.png"), full_page=True)
    page.get_by_test_id("view-submitted-job").click()
    row = page.get_by_test_id("job-row").filter(has=page.get_by_text("国漫分流验证", exact=True))
    expect(row.locator("[data-job-status]")).to_have_text("已入库", timeout=15000)
    expect(row.locator("[data-job-library]")).to_contain_text("国漫")
    assert len(list((chinese / "同名系列").glob("*.cbz"))) == 1
    assert not list(japanese.rglob("*.cbz"))

    page.set_viewport_size({"width": 1440, "height": 1080})
    page.goto(url)
    page.get_by_test_id("library-select").select_option(label="日漫")
    page.get_by_test_id("source-url").fill("https://telegra.ph/book")
    page.get_by_test_id("comic-name").fill("日漫链接分流")
    page.get_by_test_id("series-name").fill("同名系列")
    page.screenshot(path=str(ARTIFACTS / "desktop-library-selection.png"), full_page=True)
    page.get_by_test_id("submit-job").click()
    page.get_by_test_id("view-submitted-job").click()
    row = page.get_by_test_id("job-row").filter(has=page.get_by_text("日漫链接分流", exact=True))
    expect(row.locator("[data-job-status]")).to_have_text("已入库", timeout=15000)
    expect(row.locator("[data-job-library]")).to_contain_text("日漫")
    assert len(list((japanese / "同名系列").glob("*.cbz"))) == 1
    assert len(list((chinese / "同名系列").glob("*.cbz"))) == 1
    page.screenshot(path=str(ARTIFACTS / "tasks-libraries.png"), full_page=True)


@pytest.mark.parametrize("width", [320, 390, 650, 768, 1024, 1440, 1536, 1920])
def test_page_alignment_across_breakpoints(page, live_app, width):
    url, _ = live_app
    with httpx.Client(base_url=url) as client:
        if not client.get("/api/jobs").json()["jobs"]:
            library = client.get("/api/settings").json()["libraries"][0]
            response = client.post("/api/jobs/upload", files={"file": ("layout.zip", archive_bytes())},
                                   data={"library_id": library["id"], "comic_name": "排版验收"})
            assert response.status_code == 202
            assert wait_job(client, response.json()["id"])["status"] == "succeeded"
        if not any(job["available_actions"] for job in client.get("/api/jobs").json()["jobs"]):
            library = client.get("/api/settings").json()["libraries"][0]
            response = client.post("/api/jobs/upload", files={"file": ("invalid.zip", b"invalid archive")},
                                   data={"library_id": library["id"], "comic_name": "失败布局样本"})
            assert response.status_code == 202
            assert wait_job(client, response.json()["id"])["status"] == "failed"
    page.set_viewport_size({"width": width, "height": 1000})
    insets = []

    def aligned(left, right, description):
        assert abs(left - right) <= 1, f"{width}px {description}: {left} versus {right}"

    for path in ("/", "/settings", "/tasks"):
        page.goto(url + path, wait_until="networkidle")
        geometry = page.evaluate('''() => {
            const rect = selector => {
                const node = document.querySelector(selector);
                if (!node) return null;
                const value = node.getBoundingClientRect();
                return {left:value.left, right:value.right, top:value.top, bottom:value.bottom};
            };
            const legend = document.querySelector('legend');
            const range = document.createRange();
            if (legend) range.selectNodeContents(legend);
            return {
                viewport:document.documentElement.clientWidth,
                overflow:document.documentElement.scrollWidth > document.documentElement.clientWidth,
                sidebar:rect('.sidebar'), navigation:rect('.main-nav a'), main:rect('main'),
                top:rect('.workspace-topline'), heading:rect('.page-heading'),
                content:rect('.editor-layout,.settings-layout,.jobs-section'), footer:rect('.page-footer'),
                panel:rect('.editor-layout,.settings-form'),
                input:rect('#source-url,[data-testid="komga-url"]'),
                name:rect('[data-library-name]'), directory:rect('[data-library-path]'),
                job:rect('.job-list .job-row'), jobText:rect('.job-list [data-job-title]'),
                jobAction:rect('.job-list .job-actions:not([hidden])'),
                legend:legend ? range.getBoundingClientRect().left : null
            };
        }''')
        assert not geometry["overflow"], (width, path)
        for edge in ("left", "right"):
            for item in ("heading", "content", "footer"):
                aligned(geometry["top"][edge], geometry[item][edge], f"{path} {item} {edge}")
        if width > 650:
            aligned(geometry["main"]["left"] - geometry["sidebar"]["right"],
                    geometry["viewport"] - geometry["main"]["right"], f"{path} main gutters")
        else:
            aligned(geometry["navigation"]["left"], geometry["top"]["left"], f"{path} mobile gutter")
            expect(page.locator("h1")).to_be_in_viewport()
        if geometry["input"]:
            insets.append(geometry["input"]["left"] - geometry["panel"]["left"])
        if path == "/settings":
            aligned(geometry["legend"], geometry["input"]["left"], "settings legend text")
            aligned(geometry["name"]["left"], geometry["input"]["left"], "library name left")
            aligned(geometry["directory"]["right"], geometry["input"]["right"], "library path right")
            if width <= 650:
                aligned(geometry["name"]["right"], geometry["input"]["right"], "mobile library name right")
                aligned(geometry["directory"]["left"], geometry["input"]["left"], "mobile library path left")
            else:
                aligned(geometry["name"]["top"], geometry["directory"]["top"], "library input top")
                aligned(geometry["name"]["bottom"], geometry["directory"]["bottom"], "library input bottom")
        if path == "/tasks":
            aligned(geometry["jobText"]["left"] - geometry["job"]["left"], insets[0], "task text inset")
            if geometry["jobAction"]:
                aligned(geometry["job"]["right"] - geometry["jobAction"]["right"],
                        insets[0], "task action inset")
        if width in {390, 1024, 1920}:
            page.locator("h1").click()
            ARTIFACTS.mkdir(exist_ok=True)
            name = {"/": "home", "/settings": "settings", "/tasks": "tasks"}[path]
            page.screenshot(path=str(ARTIFACTS / f"alignment-{width}-{name}.png"), full_page=True)
    aligned(insets[0], insets[1], "home/settings form inset")


@pytest.mark.parametrize("width", [1024, 1440, 1920])
def test_navigation_column_matches_page_and_stays_visible(page, live_app, width):
    url, _ = live_app
    page.set_viewport_size({"width": width, "height": 720})
    for path in ("/", "/settings", "/tasks"):
        page.goto(url + path, wait_until="networkidle")
        columns = page.evaluate('''() => {
            const rect = selector => {
                const r=document.querySelector(selector).getBoundingClientRect();
                return {top:r.top,bottom:r.bottom,height:r.height};
            };
            return {sidebar:rect('.sidebar'),main:rect('main')};
        }''')
        assert abs(columns["sidebar"]["top"] - columns["main"]["top"]) <= 1
        assert abs(columns["sidebar"]["bottom"] - columns["main"]["bottom"]) <= 1, (width, path, columns)
        page.evaluate("window.scrollTo(0, document.documentElement.scrollHeight)")
        navigation = page.locator(".main-nav").bounding_box()
        assert navigation["y"] >= 0
        assert navigation["y"] + navigation["height"] <= 720
        assert page.evaluate("document.documentElement.scrollWidth <= document.documentElement.clientWidth")
        page.evaluate("window.scrollTo(0, 0)")
        if width == 1920:
            ARTIFACTS.mkdir(exist_ok=True)
            name = {"/": "home", "/settings": "settings", "/tasks": "tasks"}[path]
            page.screenshot(path=str(ARTIFACTS / f"columns-{width}-{name}.png"), full_page=True)
