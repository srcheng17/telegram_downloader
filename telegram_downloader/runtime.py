import threading
from dataclasses import dataclass


@dataclass
class AppRuntime:
    task_store: object
    app_settings: dict
    settings_lock: threading.Lock
    task_orchestrator: object
    cleanup_service: object

    def get_settings_snapshot(self):
        with self.settings_lock:
            return dict(self.app_settings)

    def update_settings(self, updates):
        with self.settings_lock:
            self.app_settings.update(updates)

    def get_setting(self, key, default=None):
        with self.settings_lock:
            return self.app_settings.get(key, default)
