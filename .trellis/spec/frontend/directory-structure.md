# Frontend Directory Structure

`telegraph_downloader/templates/` owns Jinja pages/layouts, and `static/` owns CSS and native JavaScript. FastAPI serves both from the application package, including inside the Docker image.

Keep page-specific behavior local; share code only for actual repeated transport/rendering needs. Ordinary browser navigation owns page teardown, so do not recreate the retired htmx mount/unmount dispatcher.

There is no frontend build, Node dependency or dist directory. Include source assets directly in Docker's package copy and test the image's files without a host static mount. Reuse the task design's Chinese library workspace styling, responsive navigation and readable form/task hierarchy.
