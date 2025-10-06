# DroidCam Sentry - Web Control Panel

Single-page web application for controlling the DroidCam Sentry surveillance system.

## Features

- **Camera Control**: Start/stop camera monitoring
- **Manual Recording**: Trigger recordings on-demand
- **Live Status**: Auto-refreshing system status (5s interval)
- **Recordings Browser**: Grid view of all recorded videos
- **Video Playback**: Stream recordings directly in browser
- **Download**: Save recordings to local machine

## Tech Stack

- Vanilla JavaScript (ES6+)
- HTML5 + CSS3
- Fetch API for REST calls
- No frameworks or build tools required

## Access

The web app is automatically served by the backend server:

```
http://192.168.2.149:8080/
```

## File Structure

- `index.html` - Main HTML page with embedded CSS
- `app.js` - Frontend logic and API integration

## API Integration

All API calls go through the backend REST API:
- Base URL: `http://192.168.2.149:8080`
- CORS enabled for local development
- No authentication required (trusted network)

## Development

To modify the frontend:

1. Edit `index.html` or `app.js` on nogrod
2. Refresh browser (no build step required)
3. Check browser console for errors

## Security Note

**Current design operates within trusted local network boundary (192.168.x.x)**

- No authentication/authorization
- CORS wide open (* allowed)
- Direct file system access for videos

**For external access**, implement:
- OAuth2/JWT authentication
- Restrict CORS origins
- Add rate limiting
- Use HTTPS/TLS
