// API Configuration
const API_BASE = "http://192.168.2.149:8080";

// State management
let cameras = [];
let recordings = [];
let refreshInterval = null;
let selectedRecordings = new Set();
let bulkDeleteMode = false;

// Initialize app
document.addEventListener("DOMContentLoaded", () => {
    loadCameras();
    loadRecordings();
    loadStatus();

    // Auto-refresh every 5 seconds
    refreshInterval = setInterval(() => {
        loadCameras();
        loadStatus();
        if (!bulkDeleteMode) {
            loadRecordings();
        }
    }, 5000);
});

// API Helpers
async function apiCall(endpoint, method = "GET", body = null) {
    try {
        const options = {
            method,
            headers: {
                "Content-Type": "application/json",
            },
        };

        if (body) {
            options.body = JSON.stringify(body);
        }

        const response = await fetch(`${API_BASE}${endpoint}`, options);

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || "API request failed");
        }

        return await response.json();
    } catch (error) {
        showError(error.message);
        throw error;
    }
}

// Load cameras
async function loadCameras() {
    try {
        const allCameras = await apiCall("/api/cameras");
        const status = await apiCall("/api/status");
        const statusMap = {};

        (status.cameras || []).forEach(cam => {
            statusMap[cam.name] = cam;
        });

        cameras = allCameras.map(cam => ({
            name: cam.name || cam.Name,
            description: cam.description || cam.Description || '',
            url: cam.url || cam.URL,
            enabled: cam.enabled !== undefined ? cam.enabled : cam.Enabled,
            running: statusMap[cam.name || cam.Name]?.running || false,
            recording: statusMap[cam.name || cam.Name]?.recording || false,
            motion_detection: statusMap[cam.name || cam.Name]?.motion_detection || false,
            resolution: statusMap[cam.name || cam.Name]?.resolution || null,
            fps: statusMap[cam.name || cam.Name]?.fps || null,
            codec: statusMap[cam.name || cam.Name]?.codec || null,
            health: statusMap[cam.name || cam.Name]?.health || null
        }));

        renderCameras();
    } catch (error) {
        document.getElementById("cameras-list").innerHTML =
            "<div class='empty-state'>Failed to load cameras</div>";
    }
}

// Load recordings
async function loadRecordings() {
    try {
        recordings = await apiCall("/api/recordings");
        renderRecordings();
    } catch (error) {
        document.getElementById("recordings-list").innerHTML =
            "<div class='empty-state'>Failed to load recordings</div>";
    }
}

// Load system status
async function loadStatus() {
    try {
        const status = await apiCall("/api/status");
        renderStatus(status);
    } catch (error) {
        // Silent fail for status updates
    }
}

// Render cameras
function renderCameras() {
    const container = document.getElementById("cameras-list");

    if (cameras.length === 0) {
        container.innerHTML = "<div class='empty-state'>No cameras configured</div>";
        return;
    }

    container.innerHTML = cameras.map(camera => {
        const statusBadge = camera.running ? 
            "<span class='status-badge status-running'>Running</span>" :
            "<span class='status-badge status-stopped'>Stopped</span>";
        
        const recordingBadge = camera.recording ? 
            "<span class='status-badge status-recording'>Recording</span>" : "";
        
        const motionBadge = camera.motion_detection ?
            "<span class='status-badge status-recording'>Motion Detection</span>" : "";

        // Health check icons
        const hostIcon = camera.health?.host_reachable ?
            '🟢' : '🔴';
        const urlIcon = camera.health?.url_accessible ?
            '📹' : '📵';

        const hostTitle = camera.health?.host_reachable ?
            'Host reachable' : `Host unreachable${camera.health?.host_error ? ': ' + camera.health.host_error : ''}`;
        const urlTitle = camera.health?.url_accessible ?
            `Stream accessible (${camera.health?.response_time_ms}ms)` : `Stream inaccessible${camera.health?.url_error ? ': ' + camera.health.url_error : ''}`;

        // Determine if camera can be started (both health checks must pass)
        const canStart = camera.health?.host_reachable && camera.health?.url_accessible;

        return `
        <div class="camera-card">
            <div class="camera-header">
                <div style="display: flex; justify-content: space-between; align-items: start;">
                    <div>
                        <div class="camera-name">${camera.name}</div>
                        ${camera.description ? `<div style="color: #888; font-size: 0.9em; margin-top: 4px;">${camera.description}</div>` : ''}
                    </div>
                    <div style="display: flex; gap: 8px; font-size: 1.5em;">
                        <span title="${hostTitle}">${hostIcon}</span>
                        <span title="${urlTitle}">${urlIcon}</span>
                    </div>
                </div>
                <div style="margin-top: 8px;">
                    ${statusBadge}
                    ${recordingBadge}
                    ${motionBadge}
                </div>
            </div>
            <div class="camera-details">
                <div>URL: ${camera.url}</div>
                ${camera.resolution ? `
                    <div>Video: ${camera.resolution} @ ${camera.fps ? camera.fps.toFixed(1) : 'N/A'} FPS (${camera.codec || 'Unknown'})</div>
                    <div>Audio: Not available</div>
                ` : ''}
            </div>
            <div class="camera-controls">
                <button class="btn-primary" onclick="startCamera('${camera.name}')" ${camera.running || !canStart ? "disabled" : ""}>
                    Start
                </button>
                <button class="btn-danger" onclick="stopCamera('${camera.name}')" ${!camera.running ? "disabled" : ""}>
                    Stop
                </button>
                <button class="btn-success" onclick="enableMotionDetection('${camera.name}')" ${!camera.running || camera.motion_detection ? "disabled" : ""}>
                    Motion ON
                </button>
                <button class="btn-secondary" onclick="disableMotionDetection('${camera.name}')" ${!camera.running || !camera.motion_detection ? "disabled" : ""}>
                    Motion OFF
                </button>
                <button class="btn-primary" onclick="viewLive('${camera.name}')" ${!camera.running ? "disabled" : ""}>
                    View Live
                </button>
            </div>
        </div>
    `;
    }).join("");
}

// Render status
function renderStatus(status) {
    const container = document.getElementById("system-status");

    const activeCameras = (status.cameras || []).filter(c => c.running).length;
    const recordingCameras = (status.cameras || []).filter(c => c.recording).length;

    container.innerHTML = `
        <div class="stat-item">
            <div class="stat-label">Version</div>
            <div class="stat-value">${status.version || "0.1.0"}</div>
        </div>
        <div class="stat-item">
            <div class="stat-label">Active Cameras</div>
            <div class="stat-value">${activeCameras} / ${(status.cameras || []).length}</div>
        </div>
        <div class="stat-item">
            <div class="stat-label">Recording</div>
            <div class="stat-value">${recordingCameras}</div>
        </div>
        <div class="stat-item">
            <div class="stat-label">Total Recordings</div>
            <div class="stat-value">${recordings.length}</div>
        </div>
        ${status.storage ? `
            <div class="stat-item">
                <div class="stat-label">Storage Free</div>
                <div class="stat-value">${status.storage.available_gb} GB</div>
            </div>
            <div class="stat-item">
                <div class="stat-label">Recordings Size</div>
                <div class="stat-value">${status.storage.recordings_gb} GB</div>
            </div>
        ` : ''}
    `;
}

// Render recordings
function renderRecordings() {
    const container = document.getElementById("recordings-list");

    if (recordings.length === 0) {
        container.innerHTML = "<div class='empty-state'>No recordings yet</div>";
        return;
    }

    const recItems = recordings.map(recording => `
        <div class="recording-card">
            ${bulkDeleteMode ? `
                <input type="checkbox" class="recording-checkbox"
                       ${selectedRecordings.has(recording.path) ? 'checked' : ''}
                       onchange="toggleRecordingSelection('${recording.path}')">
            ` : ''}
            <div class="recording-name">${recording.name}</div>
            <div class="recording-meta">${recording.size} • ${recording.duration || "Unknown"}</div>
            <div class="recording-meta">${recording.timestamp}</div>
            <div class="recording-actions">
                <button class="btn-primary" onclick="playRecording('${recording.path}')">
                    Play
                </button>
                <button class="btn-success" onclick="downloadRecording('${recording.path}', '${recording.name}')">
                    Download
                </button>
                ${!bulkDeleteMode ? `
                    <button class="btn-danger" onclick="deleteRecording('${recording.path}', '${recording.name}')">
                        Delete
                    </button>
                ` : ''}
            </div>
        </div>
    `).join("");

    container.innerHTML = recItems;
    updateBulkDeleteButtons();
}

// Camera controls
async function startCamera(name) {
    try {
        await apiCall(`/api/cameras/start/${name}`, "POST");
        await loadCameras();
    } catch (error) {}
}

async function stopCamera(name) {
    try {
        await apiCall(`/api/cameras/stop/${name}`, "POST");
        await loadCameras();
    } catch (error) {}
}

async function enableMotionDetection(name) {
    try {
        await apiCall(`/api/motion-detection/enable/${name}`, "POST");
        await loadCameras();
    } catch (error) {}
}

async function disableMotionDetection(name) {
    try {
        await apiCall(`/api/motion-detection/disable/${name}`, "POST");
        await loadCameras();
        setTimeout(loadRecordings, 2000); // Reload recordings after conversion
    } catch (error) {}
}

// Recording actions
function playRecording(path) {
    window.open(`${API_BASE}/api/recordings/play?file=${encodeURIComponent(path)}`, "_blank");
}

function downloadRecording(path, name) {
    const a = document.createElement("a");
    a.href = `${API_BASE}/api/recordings/download?file=${encodeURIComponent(path)}`;
    a.download = name;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
}

async function deleteRecording(path, name) {
    if (!confirm(`Delete recording "${name}"? This cannot be undone.`)) {
        return;
    }

    try {
        await apiCall(`/api/recordings/delete?file=${encodeURIComponent(path)}`, "DELETE");
        selectedRecordings.delete(path);
        await loadRecordings();
    } catch (error) {}
}

function toggleBulkDeleteMode() {
    bulkDeleteMode = !bulkDeleteMode;
    if (!bulkDeleteMode) {
        selectedRecordings.clear();
    }
    renderRecordings();
}

function selectAllRecordings() {
    if (selectedRecordings.size === recordings.length) {
        selectedRecordings.clear();
    } else {
        recordings.forEach(rec => selectedRecordings.add(rec.path));
    }
    renderRecordings();
}

function toggleRecordingSelection(path) {
    if (selectedRecordings.has(path)) {
        selectedRecordings.delete(path);
    } else {
        selectedRecordings.add(path);
    }
    renderRecordings();
}

async function deleteSelectedRecordings() {
    if (selectedRecordings.size === 0) return;

    if (!confirm(`Delete ${selectedRecordings.size} recording(s)? This cannot be undone.`)) {
        return;
    }

    const paths = Array.from(selectedRecordings);
    let deleted = 0;
    let failed = 0;

    for (const path of paths) {
        try {
            await apiCall(`/api/recordings/delete?file=${encodeURIComponent(path)}`, "DELETE");
            deleted++;
        } catch (error) {
            failed++;
        }
    }

    selectedRecordings.clear();
    await loadRecordings();

    if (failed > 0) {
        showError(`Deleted ${deleted} recording(s), ${failed} failed`);
    }
}

function updateBulkDeleteButtons() {
    const toggleBtn = document.getElementById("bulk-toggle-btn");
    const selectAllBtn = document.getElementById("select-all-btn");
    const deleteBtn = document.getElementById("bulk-delete-btn");

    toggleBtn.textContent = bulkDeleteMode ? "Disable Bulk Delete" : "Enable Bulk Delete";
    toggleBtn.className = bulkDeleteMode ? "btn-success" : "btn-secondary";

    selectAllBtn.style.display = bulkDeleteMode ? "inline-block" : "none";
    selectAllBtn.textContent = selectedRecordings.size === recordings.length ? "Deselect All" : "Select All";

    deleteBtn.style.display = bulkDeleteMode ? "inline-block" : "none";
    deleteBtn.disabled = selectedRecordings.size === 0;
    deleteBtn.textContent = `Delete Selected (${selectedRecordings.size})`;
}

// Live camera viewing
function viewLive(cameraName) {
    window.open(`live.html?camera=${cameraName}`, `live-${cameraName}`, 'width=800,height=600');
}

// Error handling
function showError(message) {
    const container = document.getElementById("error-container");
    container.innerHTML = `<div class="error">${message}</div>`;

    setTimeout(() => {
        container.innerHTML = "";
    }, 5000);
}

// Cleanup on page unload
window.addEventListener("beforeunload", () => {
    if (refreshInterval) {
        clearInterval(refreshInterval);
    }
});
