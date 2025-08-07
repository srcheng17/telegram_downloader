import os
from flask import Flask, render_template, request, redirect, url_for, jsonify, session
from downloader_logic import download_images
from concurrent.futures import ThreadPoolExecutor
import time
import uuid
import threading
import atexit
import logging

app = Flask(__name__)

# --- Logging Setup ---
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(levelname)s - %(message)s')
# --- End Logging Setup ---
app.secret_key = os.environ.get('SECRET_KEY', os.urandom(24))

@app.context_processor
def inject_static_url_helpers():
    """
    Injects a helper function into the template context that adds a cache-busting
    query parameter to static file URLs.
    """
    def url_for_static_bust_cache(filename):
        # Use the file's modification time as the cache buster
        filepath = os.path.join(app.root_path, app.static_folder, filename)
        if os.path.exists(filepath):
            bust = int(os.path.getmtime(filepath))
            return url_for('static', filename=filename, v=bust)
        return url_for('static', filename=filename)
    return dict(url_for_static_bust_cache=url_for_static_bust_cache)

# In-memory data stores
tasks = {}
app_settings = {
    'concurrency': 2,
    'timeout': 30,
    'retries': 10,
    'log_retention_days': 7
}

# Initialize the thread pool executor
executor = ThreadPoolExecutor(max_workers=app_settings['concurrency'])

# --- Background Task for Log Cleanup ---
def clean_old_logs():
    """Periodically cleans up old tasks from the in-memory dictionary."""
    while True:
        time.sleep(3600) # Run once per hour
        retention_days = session.get('settings', app_settings).get('log_retention_days')
        retention_seconds = retention_days * 86400
        current_time = time.time()
        
        tasks_to_delete = [
            task_id for task_id, task in tasks.items()
            if (current_time - task['start_time']) > retention_seconds
        ]
        
        for task_id in tasks_to_delete:
            del tasks[task_id]

cleanup_thread = threading.Thread(target=clean_old_logs, daemon=True)
cleanup_thread.start()
# --- End Background Task ---

def update_concurrency():
    """Updates the thread pool executor's max workers."""
    global executor
    new_concurrency = session.get('settings', app_settings).get('concurrency')
    if executor._max_workers != new_concurrency:
        executor.shutdown(wait=True)
        executor = ThreadPoolExecutor(max_workers=new_concurrency)

def run_download(task_id, url, timeout, retries):
    """Wrapper function to run in a thread and update task status."""
    try:
        app.logger.info(f"Task {task_id} - Status: IN_PROGRESS")
        tasks[task_id]['status'] = 'IN_PROGRESS'
        
        download_images(url, timeout=timeout, retries=retries, task_id=task_id, tasks_db=tasks)
        
        app.logger.info(f"Task {task_id} - Status: SUCCESS")
        tasks[task_id]['status'] = 'SUCCESS'
    except Exception as e:
        app.logger.error(f"Task {task_id} - Status: FAILED, Error: {e}")
        tasks[task_id]['status'] = 'FAILED'
        tasks[task_id]['error'] = str(e)

@app.route('/')
def index():
    return render_template('index.html')

@app.route('/download', methods=['POST'])
def download():
    url = request.form.get('url')
    if not url:
        return redirect(url_for('index'))

    task_id = str(uuid.uuid4())
    settings = session.get('settings', app_settings)
    tasks[task_id] = {
        'id': task_id,
        'url': url,
        'status': 'PENDING',
        'start_time': time.time(),
        'error': None,
        'progress': 0,
        'total_images': 0,
        'concurrency': settings['concurrency']
    }
    
    app.logger.info(f"New task created: {task_id} for URL: {url}")

    executor.submit(run_download, task_id, url, settings['timeout'], settings['retries'])
    return redirect(url_for('logs'))

@app.route('/logs')
def logs():
    return render_template('logs.html')

@app.route('/api/logs')
def api_logs():
    page = request.args.get('page', 1, type=int)
    per_page = request.args.get('per_page', 25, type=int)
    
    sorted_logs = sorted(tasks.values(), key=lambda x: x['start_time'], reverse=True)
    
    start = (page - 1) * per_page
    end = start + per_page
    
    paginated_logs = sorted_logs[start:end]
    
    return jsonify({
        'logs': paginated_logs,
        'total': len(sorted_logs),
        'page': page,
        'per_page': per_page
    })

@app.route('/settings', methods=['GET', 'POST'])
def settings():
    if request.method == 'POST':
        current_settings = session.get('settings', app_settings.copy())
        current_settings['concurrency'] = int(request.form.get('concurrency'))
        current_settings['timeout'] = int(request.form.get('timeout'))
        current_settings['retries'] = int(request.form.get('retries'))
        current_settings['log_retention_days'] = int(request.form.get('log_retention_days'))
        session['settings'] = current_settings
        
        app.logger.info(f"Settings updated: {current_settings}")
        
        update_concurrency()
        
        return redirect(url_for('settings'))

    return render_template('settings.html', settings=session.get('settings', app_settings))
