import threading
from dataclasses import dataclass


def _default_startup_recovery():
    return {
        "happened": False,
        "recovered_total": 0,
        "recovered_failed": 0,
        "recovered_canceled": 0,
        "occurred_at": None,
    }


def _safe_int(value, default=0):
    try:
        return int(value)
    except (TypeError, ValueError):
        return default


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

    def set_startup_recovery(self, recovery_summary):
        payload = recovery_summary or {}
        normalized = _default_startup_recovery()
        normalized["happened"] = bool(payload.get("happened", False))
        normalized["recovered_total"] = max(0, _safe_int(payload.get("recovered_total"), 0))
        normalized["recovered_failed"] = max(0, _safe_int(payload.get("recovered_failed"), 0))
        normalized["recovered_canceled"] = max(0, _safe_int(payload.get("recovered_canceled"), 0))
        normalized["occurred_at"] = payload.get("occurred_at")

        with self.settings_lock:
            self.startup_recovery = normalized

    def get_startup_recovery(self):
        with self.settings_lock:
            if not self.startup_recovery:
                return _default_startup_recovery()
            return dict(self.startup_recovery)
