# droidcam-sentry

Cheap surveillance system using DroidCam phones for motion detection and recording.

## Features

- Connect to multiple DroidCam IP cameras
- Motion detection and automatic recording
- REST API for configuration and control
- Hot-reload configuration without restart
- Configurable motion sensitivity and recording parameters

## Quick Start

```bash
# Install dependencies
go mod download

# Build
go build -o droidcam-sentry

# Run
./droidcam-sentry
```

## Configuration

Edit `config.yaml` to configure cameras and settings:

```yaml
cameras:
  - name: "droidcam-1"
    url: "http://192.168.9.184:4747/video"
    enabled: true
    motion_threshold: 25.0
```

## API Endpoints

- `GET /health` - Health check
- `GET /api/config` - Get current configuration
- `PUT /api/config` - Update configuration
- `GET /api/cameras` - List cameras
- `PUT /api/cameras/{name}` - Update camera settings
- `GET /api/status` - System status
- `GET /api/recordings` - List recordings

### Example: Update camera URL

```bash
curl -X PUT http://localhost:8080/api/cameras/droidcam-1 \
  -H "Content-Type: application/json" \
  -d '{"url": "http://192.168.9.184:4747/video"}'
```

### Example: Enable/disable camera

```bash
curl -X PUT http://localhost:8080/api/cameras/droidcam-1 \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

## TODO

- [ ] Implement GoCV integration for video capture
- [ ] Implement motion detection
- [ ] Implement video recording with pre/post buffers
- [ ] Add ONNX person detection
- [ ] Add web UI for viewing recordings
- [ ] Add authentication
