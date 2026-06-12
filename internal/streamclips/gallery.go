package streamclips

import (
	"html"
	"net/url"
	"strings"
)

// RenderGalleryHTML builds the review gallery for one stream clip render.
func RenderGalleryHTML(j Job, videos []VideoEntry) string {
	title := strings.TrimSpace(j.Title)
	if title == "" {
		title = "Streamer clips"
	}

	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</title></head><body><h1>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</h1>")
	for _, video := range videos {
		clipID := html.EscapeString(video.ClipID)
		videoPath := html.EscapeString(url.PathEscape(video.ClipID) + ".mp4")
		b.WriteString("<section><h2>")
		b.WriteString(clipID)
		b.WriteString("</h2><video controls src=\"videos/")
		b.WriteString(videoPath)
		b.WriteString("\"></video></section>")
	}
	b.WriteString("</body></html>")
	return b.String()
}
