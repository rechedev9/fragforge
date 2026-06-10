package httpapi

import (
	"embed"
	"fmt"
	"net/http"
	"strings"
)

//go:embed workbench_assets/index.html workbench_assets/styles.css workbench_assets/app.js
var workbenchAssets embed.FS

// Workbench serves the local operator UI. The app intentionally talks only to
// the local HTTP API and does not load third-party scripts or assets.
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
	js, err := workbenchAssets.ReadFile("workbench_assets/app.js")
	if err != nil {
		return "", fmt.Errorf("read workbench js: %w", err)
	}
	doc := strings.ReplaceAll(string(html), "{{WORKBENCH_CSS}}", string(css))
	doc = strings.ReplaceAll(doc, "{{WORKBENCH_JS}}", string(js))
	return doc, nil
}
