// Package adminui serves the self-contained administrator console.
package adminui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html app.css app.js i18n.js favicon.svg
var assets embed.FS

// Handler returns the embedded console. All data operations use authenticated
// administrative endpoints; the assets themselves contain no credentials.
func Handler() http.Handler {
	root, err := fs.Sub(assets, ".")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		http.StripPrefix("/admin", files).ServeHTTP(w, r)
	})
}
