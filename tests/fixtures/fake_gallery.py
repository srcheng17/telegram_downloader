"""Deterministic CLI boundary failures; successful extraction uses the real runner."""

import json
import os
import subprocess
import sys
import time
from pathlib import Path
from urllib.parse import urlsplit

arguments = sys.argv[1:]
assert "--config-ignore" in arguments
assert "--no-input" in arguments
directory = Path(arguments[arguments.index("-D") + 1])
directory.mkdir(parents=True, exist_ok=True)
scenario = urlsplit(arguments[-1]).path.strip("/")

if scenario in {"hang", "budget"}:
    if scenario == "budget":
        (directory / "00001.png.part").write_bytes(b"x" * 4096)
    marker = Path(os.environ["MEDIA_FIXTURE_MARKER"])
    child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(60)"])
    marker.write_text(json.dumps({"parent": os.getpid(), "child": child.pid}))
    time.sleep(60)

count = 2
if scenario == "empty":
    count = 0
elif scenario == "bool":
    count = True
elif scenario == "many":
    count = 301
(directory / "info.json").write_text(json.dumps({"count": count}))
if scenario != "empty":
    (directory / "00001.png").write_bytes(b"page one")
if scenario not in {"missing", "partial", "empty"}:
    (directory / "00002.png").write_bytes(b"page two")
if scenario == "part":
    (directory / "00003.png.part").write_bytes(b"unfinished")
if scenario == "unexpected":
    (directory / "other.txt").write_text("unexpected")
if scenario == "partial":
    raise SystemExit(4)
