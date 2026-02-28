import os

from flask import url_for


def register_template_helpers(app):
    @app.context_processor
    def inject_static_url_helpers():
        """Inject a helper that adds cache-busting query params for static files."""

        def url_for_static_bust_cache(filename):
            filepath = os.path.join(app.static_folder, filename)
            if os.path.exists(filepath):
                bust = int(os.path.getmtime(filepath))
                return url_for("static", filename=filename, v=bust)
            return url_for("static", filename=filename)

        return {"url_for_static_bust_cache": url_for_static_bust_cache}
