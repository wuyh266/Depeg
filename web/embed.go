// Package web serves the application from the Go executable without a Node runtime.
package web

import (
	"embed"
	"net/http"
)

//go:embed static/*
var files embed.FS

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := "static/index.html"
		contentType := "text/html; charset=utf-8"
		switch r.URL.Path {
		case "/":
		case "/static/app.js":
			name = "static/app.js"
			contentType = "text/javascript; charset=utf-8"
		case "/static/charts.js":
			name = "static/charts.js"
			contentType = "text/javascript; charset=utf-8"
		case "/static/app.css":
			name = "static/app.css"
			contentType = "text/css; charset=utf-8"
		default:
			http.NotFound(w, r)
			return
		}
		body, err := files.ReadFile(name)
		if err != nil {
			http.Error(w, "Web assets unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.WriteHeader(200)
		_, _ = w.Write(body)
	})
}
