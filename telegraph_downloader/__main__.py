import uvicorn

uvicorn.run("telegraph_downloader.main:app", host="0.0.0.0", port=5000, workers=1)
