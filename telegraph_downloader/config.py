from __future__ import annotations

import os
import sys
from dataclasses import dataclass, field
from pathlib import Path

MIB = 1024 * 1024


@dataclass(frozen=True)
class Config:
    data_dir: Path = field(default_factory=lambda: Path(os.environ.get("DATA_DIR", "data/single-host")))
    komga_dir: Path | None = field(default_factory=lambda: Path(os.environ["KOMGA_DIR"]) if os.environ.get("KOMGA_DIR") else None)
    task_timeout: float = 900
    max_upload_bytes: int = 64 * MIB
    max_images: int = 300
    max_image_bytes: int = 25 * MIB
    max_total_bytes: int = 500 * MIB
    gallery_command: tuple[str, ...] = (sys.executable, "-m", "gallery_dl")

    def prepare(self) -> None:
        self.data_dir.mkdir(parents=True, exist_ok=True)
        for name in ("inputs", "work", "artifacts"):
            path = self.data_dir / name
            if path.is_symlink():
                raise ValueError("数据子目录不能是符号链接")
            path.mkdir(exist_ok=True)
