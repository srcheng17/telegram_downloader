import threading
from dataclasses import dataclass


@dataclass
class AppRuntime:
    task_store: object
    app_settings: dict
    settings_lock: threading.Lock
    task_orchestrator: object
    cleanup_service: object
    recovery_service: object = None
    startup_recovery: dict = None

    def get_settings_snapshot(self):
        with self.settings_lock:
            return dict(self.app_settings)

    def update_settings(self, updates):
        with self.settings_lock:
            self.app_settings.update(updates)

    def get_setting(self, key, default=None):
        with self.settings_lock:
            return self.app_settings.get(key, default)

    def get_startup_recovery(self):
        snapshot = self.startup_recovery
        if not isinstance(snapshot, dict):
            return {
                "happened": False,
                "recovered_total": 0,
                "recovered_failed": 0,
                "recovered_canceled": 0,
                "occurred_at": None,
            }
        return {
            "happened": bool(snapshot.get("happened", False)),
            "recovered_total": int(snapshot.get("recovered_total", 0) or 0),
            "recovered_failed": int(snapshot.get("recovered_failed", 0) or 0),
            "recovered_canceled": int(snapshot.get("recovered_canceled", 0) or 0),
            "occurred_at": snapshot.get("occurred_at"),
        }

    def set_startup_recovery(self, snapshot):
        if not isinstance(snapshot, dict):
            self.startup_recovery = None
            return
        self.startup_recovery = {
            "happened": bool(snapshot.get("happened", False)),
            "recovered_total": int(snapshot.get("recovered_total", 0) or 0),
            "recovered_failed": int(snapshot.get("recovered_failed", 0) or 0),
            "recovered_canceled": int(snapshot.get("recovered_canceled", 0) or 0),
            "occurred_at": snapshot.get("occurred_at"),
        }
