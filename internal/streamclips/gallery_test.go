package streamclips

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestRenderGalleryHTMLEscapesTextAndUsesVideoPaths(t *testing.T) {
	html := RenderGalleryHTML(Job{
		ID:    uuid.New(),
		Title: `Frag <Run>`,
	}, []VideoEntry{
		{
			ClipID: "clip-001",
			Title:  "ignored for now",
			Key:    "stream-jobs/id/renders/variant/videos/clip-001.mp4",
		},
		{
			ClipID: `clip "two"`,
			Key:    "stream-jobs/id/renders/variant/videos/clip-two.mp4",
		},
	})

	for _, want := range []string{
		"<title>Frag &lt;Run&gt;</title>",
		"<h1>Frag &lt;Run&gt;</h1>",
		"<h2>clip-001</h2>",
		`src="videos/clip-001.mp4"`,
		"<h2>clip &#34;two&#34;</h2>",
		`src="videos/clip%20%22two%22.mp4"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("gallery html missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "Frag <Run>") {
		t.Fatalf("gallery html did not escape title:\n%s", html)
	}
}

func TestRenderGalleryHTMLUsesFallbackTitle(t *testing.T) {
	html := RenderGalleryHTML(Job{}, nil)
	if !strings.Contains(html, "<title>Streamer clips</title>") || !strings.Contains(html, "<h1>Streamer clips</h1>") {
		t.Fatalf("gallery html missing fallback title:\n%s", html)
	}
}
