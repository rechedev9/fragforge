package httpapi

import (
	"embed"
	"fmt"
	"net/http"
	"strings"
)

//go:embed workbench_assets/index.html workbench_assets/styles.css workbench_assets/htmx.css workbench_assets/htmx.min.js
var workbenchAssets embed.FS

// Workbench serves the local operator UI. It is a Go-rendered HTMX console that
// talks only to same-origin endpoints and does not require the Next/TypeScript
// frontend to be running.
func (h *Handlers) Workbench(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	doc, err := workbenchDocument()
	if err != nil {
		internalError(w, "render workbench", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(doc))
}

func workbenchDocument() (string, error) {
	html, err := workbenchAssets.ReadFile("workbench_assets/index.html")
	if err != nil {
		return "", fmt.Errorf("read workbench html: %w", err)
	}
	css, err := workbenchAssets.ReadFile("workbench_assets/styles.css")
	if err != nil {
		return "", fmt.Errorf("read workbench css: %w", err)
	}
	htmxCSS, err := workbenchAssets.ReadFile("workbench_assets/htmx.css")
	if err != nil {
		return "", fmt.Errorf("read workbench htmx css: %w", err)
	}
	htmx, err := workbenchAssets.ReadFile("workbench_assets/htmx.min.js")
	if err != nil {
		return "", fmt.Errorf("read htmx: %w", err)
	}
	doc := strings.ReplaceAll(string(html), "{{WORKBENCH_CSS}}", string(css)+"\n"+string(htmxCSS))
	doc = strings.ReplaceAll(doc, "{{WORKBENCH_HTMX}}", string(htmx))
	return doc, nil
}
