import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from metadata_probe import preview


def message(mid, text="", file="", **raw):
    return {"id": mid, "text": text, "file": file, "raw": raw}


def sample():
    return {"id": 1234567890123456789, "messages": [
        message(65, "《另一个故事》\n另一段简介\n\n#历史"),
        message(68, "[另一社团(另一作者)]另一个故事[译组]", "other.zip"),
        message(69, "《另一个故事》\n在线阅读：链接", Entities=[{"URL": "https://telegra.ph/other"}]),
        message(70, "《纸飞机》\n第一段 🛫\n第二段❤\n第三段\n第四段\n\n#旅行 #友情 #旅行", GroupedID=987654321012345678),
        message(71, GroupedID=987654321012345678),
        message(72, GroupedID=987654321012345678),
        message(73, "[纸社(笔名)]Paper Plane[译组]\n[#纸社(#笔名)]纸飞机[#译组]", "paper.zip"),
        message(74, "《纸飞机》\n在线阅读：链接", Entities=[{"Offset": 13, "Length": 2, "URL": "https://telegra.ph/paper"}]),
    ]}


class PreviewTests(unittest.TestCase):
    def test_exact_title_excludes_adjacent_book_and_keeps_int64_album_separate(self):
        result = preview(sample(), 70)
        self.assertEqual(result["candidates"][0]["metadata"]["author"], "笔名")
        self.assertEqual(result["candidates"][0]["metadata"]["comic_name"], "纸飞机")
        self.assertEqual(result["candidates"][0]["download_url"], "https://telegra.ph/paper")
        self.assertEqual(result["album"]["message_ids"], [70, 71, 72])
        self.assertEqual(result["album"]["grouped_id"], 987654321012345678)
        self.assertEqual(result["source"]["channel_id"], 1234567890123456789)
        self.assertEqual(result["candidates"][0]["evidence"]["author"]["message_ids"], [70, 73])
        self.assertFalse({"series_name", "series_number", "genres"} & result["candidates"][0]["metadata"].keys())

    def test_summary_preserves_emoji_and_removes_only_trailing_tag_lines(self):
        data = sample()
        data["messages"][3]["text"] = "《纸飞机》\n第一段 🛫\n正文中的 #词 保留\n\n第二段❤\n\n#旅行 #友情\n#旅行 #日常\n"
        result = preview(data, 70)
        self.assertEqual(result["candidates"][0]["metadata"]["summary"], "第一段 🛫\n正文中的 #词 保留\n\n第二段❤")
        self.assertEqual(result["candidates"][0]["metadata"]["tags"], "旅行,友情,日常")

    def test_duplicate_zip_matches_require_selection_even_with_same_artist(self):
        data = sample()
        duplicate = copy.deepcopy(data["messages"][6])
        duplicate["id"] = 75
        data["messages"].append(duplicate)
        result = preview(data, 70)
        self.assertEqual(result["outcome"], "needs_selection")
        self.assertNotIn("author", result["candidates"][0]["metadata"])
        self.assertEqual([c["message_id"] for c in result["zip_candidates"]], [73, 75])

    def test_missing_and_conflicting_artist_never_infer_circle_or_translator(self):
        for credit in ("纸社", "纸社(不同作者)", "纸社()"):
            with self.subTest(credit=credit):
                data = sample()
                data["messages"][6]["text"] = f"[{credit}]Paper Plane[译组]\n[#纸社(#笔名)]纸飞机[#译组]"
                result = preview(data, 70)
                self.assertNotIn("author", result["candidates"][0]["metadata"])
                self.assertEqual(result["candidates"][0]["warnings"][0]["code"], "zip_artist_missing_or_conflicting")
        data = sample()
        data["messages"][6]["text"] = "[纸社(笔名)]另一个标题[译组]"
        self.assertNotIn("author", preview(data, 70)["candidates"][0]["metadata"])

    def test_hidden_url_allowlist_ignores_offsets_and_rejects_deceptive_urls(self):
        for url in ("https://telegra.ph.evil.test/paper", "https://telegra.ph@evil.test/paper",
                    "javascript:https://telegra.ph/paper", "https://evil.test/telegra.ph", "https://telegra.ph:8443/paper"):
            with self.subTest(url=url):
                data = sample()
                data["messages"][7]["raw"]["Entities"][0]["URL"] = url
                self.assertNotIn("download_url", preview(data, 70)["candidates"][0])
        data = sample()
        data["messages"].append(message(75, "《纸飞机》\n链接", Entities=[{"URL": "https://graph.org/second"}]))
        result = preview(data, 70)
        self.assertEqual(result["outcome"], "needs_selection")
        self.assertNotIn("download_url", result["candidates"][0])

    def test_missing_target_or_title_and_utf8_field_limit(self):
        with self.assertRaises(ValueError):
            preview(sample(), 999)
        self.assertEqual(preview(sample(), 71)["outcome"], "empty")
        data = sample()
        data["messages"][3]["text"] = "《纸飞机》\n" + "🛫界" * 1000
        result = preview(data, 70)
        self.assertLessEqual(len(result["candidates"][0]["metadata"]["summary"].encode("utf-8")), 4000)
        self.assertIn("field_truncated:summary", [w["code"] for w in result["candidates"][0]["warnings"]])

    def test_cli_writes_private_json_and_prints_no_field_values(self):
        with tempfile.TemporaryDirectory() as temp:
            source, output = Path(temp) / "source.json", Path(temp) / "preview.json"
            source.write_text(json.dumps(sample()), encoding="utf-8")
            command = [sys.executable, "-B", str(Path(__file__).with_name("metadata_probe.py")),
                       "--input", str(source), "--target", "70", "--output", str(output)]
            run = subprocess.run(command, check=True, capture_output=True, text=True)
            self.assertEqual(os.stat(output).st_mode & 0o777, 0o600)
            self.assertIn("author", json.loads(run.stdout)["fields"])
            self.assertNotIn("纸飞机", run.stdout + run.stderr)
            self.assertNotIn("telegra.ph/paper", run.stdout + run.stderr)
            self.assertEqual(json.loads(output.read_text())["source"]["channel_id"], sample()["id"])
            self.assertNotEqual(subprocess.run(command, capture_output=True).returncode, 0)


if __name__ == "__main__":
    unittest.main()
