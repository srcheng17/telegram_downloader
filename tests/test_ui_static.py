from html.parser import HTMLParser
from pathlib import Path

from jinja2 import Environment, FileSystemLoader, select_autoescape

TEMPLATES = Path(__file__).resolve().parents[1] / "telegraph_downloader" / "templates"


class Page(HTMLParser):
    def __init__(self, html):
        super().__init__()
        self.elements = []
        self.text = []
        self.form_fields = {}
        self.current_form = None
        self.feed(html)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        self.elements.append((tag, attrs))
        if tag == "form":
            self.current_form = attrs.get("id")
            self.form_fields[self.current_form] = []
        if tag in {"input", "textarea", "select"} and self.current_form and "disabled" not in attrs:
            self.form_fields[self.current_form].append(attrs.get("name"))

    def handle_endtag(self, tag):
        if tag == "form":
            self.current_form = None

    def handle_data(self, data):
        self.text.append(data)


def render(name, jobs=None, **context):
    env = Environment(
        loader=FileSystemLoader(TEMPLATES), autoescape=select_autoescape(["html"])
    )
    values = {
        "active_page": {"index.html": "home", "tasks.html": "tasks"}.get(name, "settings"),
        "settings": {"libraries": [{"id": "default", "name": "默认书库", "path": "/komga"}]},
        "komga_enabled": True, "jobs": jobs or [], **context,
    }
    return Page(env.get_template(name).render(**values))


def job(actions, **values):
    return {
        "id": "test-job",
        "kind": "url",
        "source": "https://telegra.ph/example",
        "metadata": {"comic_name": "测试漫画"},
        "status": "succeeded",
        "status_label": "已入库",
        "available_actions": actions,
        **values,
    }


def test_task_text_cannot_create_html_or_scripts():
    attack = '<img src=x onerror="alert(1)"><script>alert(2)</script>'
    page = render(
        "tasks.html",
        [job([], metadata={"comic_name": attack}, error=attack)],
    )
    assert attack in "".join(page.text)
    assert not [tag for tag, _ in page.elements if tag == "img"]
    scripts = [attrs for tag, attrs in page.elements if tag == "script"]
    assert scripts == [{"src": "/static/app.js", "type": "module"}]


def test_task_controls_follow_backend_and_exclude_manual_delivery():
    for actions in [["cancel"], ["retry"], [], ["download", "komga"]]:
        page = render("tasks.html", [job(actions)])
        controls = [
            attrs
            for _, attrs in page.elements
            if "data-action" in attrs
        ][:2]
        expected = [action for action in actions if action in {"cancel", "retry"}]
        assert [attrs["data-action"] for attrs in controls if "hidden" not in attrs] == expected


def test_home_only_contains_submission_and_receipt():
    page = render("index.html", [job([])])
    assert "archive-form" in page.form_fields
    assert not any("data-jobs" in attrs or "data-job-list" in attrs for _, attrs in page.elements)
    assert not any(attrs.get("data-job-id") for _, attrs in page.elements)
    receipt = [attrs for tag, attrs in page.elements if attrs.get("data-testid") == "view-submitted-job"]
    assert len(receipt) == 1 and receipt[0].get("href") == "/tasks"


def test_no_page_offers_browser_download_or_manual_komga_delivery():
    for name in ["index.html", "tasks.html", "settings.html"]:
        page = render(name, [job(["download", "komga"])])
        assert not any(attrs.get("data-action") in {"download", "komga"} for _, attrs in page.elements)
        assert not any(attrs.get("name") == "default_action" for _, attrs in page.elements)
        assert "下载" not in "".join(page.text)


def test_komga_configuration_is_required_and_url_is_optional():
    home = render("index.html", komga_enabled=False)
    submit = next(attrs for _, attrs in home.elements if attrs.get("data-testid") == "submit-job")
    assert "disabled" in submit
    settings = render("settings.html")
    fields = {attrs.get("name"): attrs for tag, attrs in settings.elements if tag == "input"}
    assert set(fields) == {"komga_url", "library_name", "library_path"}
    assert fields["komga_url"]["type"] == "url" and "required" not in fields["komga_url"]
    assert "required" in fields["library_name"] and "required" in fields["library_path"]


def test_metadata_is_optional_and_inputs_have_visible_labels():
    page = render("index.html")
    metadata = {
        "author", "series_name", "series_number", "comic_name", "summary", "tags", "genres"
    }
    fields = [attrs for tag, attrs in page.elements if tag in {"input", "textarea"}]
    optional = [attrs for attrs in fields if attrs.get("name") in metadata]
    labels = {attrs.get("for") for tag, attrs in page.elements if tag == "label"}
    assert {attrs["name"] for attrs in optional} == metadata
    assert all("required" not in attrs and attrs["id"] in labels for attrs in optional)
    assert set(page.form_fields["archive-form"]) == metadata | {"url", "library_id"}
    assert any(tag == "html" and attrs.get("lang") == "zh-CN" for tag, attrs in page.elements)


def test_empty_pages_render_without_invented_tasks():
    for name in ["index.html", "tasks.html", "settings.html"]:
        page = render(name)
        assert any(tag == "h1" for tag, _ in page.elements)
        assert not any(attrs.get("data-job-id") for _, attrs in page.elements)
        external_assets = [
            attrs for _, attrs in page.elements
            if attrs.get("src", "").startswith("https://")
            or attrs.get("href", "").startswith("https://")
        ]
        assert external_assets == []


def test_library_selection_is_automatic_only_with_one_library():
    libraries = [
        {"id": "japan", "name": "日漫", "path": "/komga/日漫"},
        {"id": "china", "name": "国漫", "path": "/komga/国漫"},
    ]
    for count in [1, 2]:
        page = render("index.html", settings={"libraries": libraries[:count]})
        select = next(attrs for tag, attrs in page.elements if tag == "select")
        assert select["name"] == "library_id" and "required" in select
        options = [attrs for tag, attrs in page.elements if tag == "option"]
        selected = [attrs.get("value") for attrs in options if "selected" in attrs]
        assert selected == (["japan"] if count == 1 else [""])


def test_settings_preserves_ids_and_escapes_library_text():
    attack = '<img src=x onerror="alert(1)">'
    page = render("settings.html", settings={"libraries": [
        {"id": "japan", "name": "日漫", "path": "/komga/日漫"},
        {"id": "china", "name": attack, "path": "/komga/国漫"},
    ]})
    ids = [attrs.get("data-library-id") for _, attrs in page.elements if attrs.get("data-library-id")]
    assert ids == ["japan", "china"]
    assert not any(tag == "img" for tag, _ in page.elements)
    assert any(attrs.get("value") == attack for tag, attrs in page.elements if tag == "input")


def test_task_records_its_target_library_name():
    page = render("tasks.html", [job([], library_id="china", library_name="国漫")])
    assert any("data-job-library" in attrs for _, attrs in page.elements)
    assert "国漫" in "".join(page.text)
