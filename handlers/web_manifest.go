package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ManifestPath is the web app manifest, which lets phones add the app to
// the home screen (#65). It needs no session, so the icon can be fetched
// before login.
const ManifestPath = AppHome + "/manifest.webmanifest"

// webManifest opens /app without browser bars. The colours are the light
// theme's; the theme-color meta tags in app.html add the dark one. There is
// no service worker: the app is online only (ADR 6).
const webManifest = `{
	"name": "Scribble",
	"short_name": "Scribble",
	"id": "/app",
	"start_url": "/app",
	"scope": "/app",
	"display": "standalone",
	"theme_color": "#ffffff",
	"background_color": "#f8fafc",
	"icons": [
		{"src": "/assets/img/icon-192.png", "sizes": "192x192", "type": "image/png", "purpose": "any"},
		{"src": "/assets/img/icon-512.png", "sizes": "512x512", "type": "image/png", "purpose": "any"},
		{"src": "/assets/img/icon-maskable-512.png", "sizes": "512x512", "type": "image/png", "purpose": "maskable"}
	]
}
`

// AppManifest serves the web app manifest.
func (h *Handler) AppManifest(c *gin.Context) error {
	c.Data(http.StatusOK, "application/manifest+json", []byte(webManifest))
	return nil
}
