package renderplan

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewPublishBoardMarksReadyWhenAllItemsReady(t *testing.T) {
	board := NewPublishBoard(NewPublishBoardOptions{
		JobID:   uuid.New(),
		Variant: "natural-hq2-full",
		Items: []PublishBoardItem{{
			SegmentID:    "seg-001",
			VideoReady:   true,
			CoverReady:   true,
			CaptionReady: true,
		}},
	})

	if board.Status != "ready" || !board.RenderReady {
		t.Fatalf("status/render_ready = %q/%v, want ready/true", board.Status, board.RenderReady)
	}
	if board.Items[0].Status != "ready" {
		t.Fatalf("item status = %q, want ready", board.Items[0].Status)
	}
}

func TestNewPublishBoardSurfacesMissingCoverBeforeCaption(t *testing.T) {
	board := NewPublishBoard(NewPublishBoardOptions{
		JobID:   uuid.New(),
		Variant: "natural-hq2-full",
		Items: []PublishBoardItem{{
			SegmentID:    "seg-001",
			VideoReady:   true,
			CaptionReady: false,
		}},
	})

	if board.Status != "needs_cover" {
		t.Fatalf("status = %q, want needs_cover", board.Status)
	}
	if board.Items[0].Status != "needs_cover" {
		t.Fatalf("item status = %q, want needs_cover", board.Items[0].Status)
	}
}

func TestNewPublishBoardMarksNeedsCaption(t *testing.T) {
	board := NewPublishBoard(NewPublishBoardOptions{
		JobID:   uuid.New(),
		Variant: "natural-hq2-full",
		Items: []PublishBoardItem{{
			SegmentID:    "seg-001",
			VideoReady:   true,
			CoverReady:   true,
			CaptionReady: false,
		}},
	})

	if board.Status != "needs_caption" {
		t.Fatalf("status = %q, want needs_caption", board.Status)
	}
}

func TestNewPublishBoardMarksFailedFromRenderError(t *testing.T) {
	board := NewPublishBoard(NewPublishBoardOptions{
		JobID:   uuid.New(),
		Variant: "natural-hq2-full",
		Error:   "render failed",
		Items: []PublishBoardItem{{
			SegmentID: "seg-001",
		}},
	})

	if board.Status != "failed" || board.RenderReady {
		t.Fatalf("status/render_ready = %q/%v, want failed/false", board.Status, board.RenderReady)
	}
}

func TestNewPublishBoardMarksUploadedWhenReadyAndUploaded(t *testing.T) {
	board := NewPublishBoard(NewPublishBoardOptions{
		JobID:    uuid.New(),
		Variant:  "natural-hq2-full",
		Uploaded: true,
		Items: []PublishBoardItem{{
			SegmentID:    "seg-001",
			VideoReady:   true,
			CoverReady:   true,
			CaptionReady: true,
		}},
	})

	if board.Status != "uploaded" || !board.Uploaded {
		t.Fatalf("status/uploaded = %q/%v, want uploaded/true", board.Status, board.Uploaded)
	}
}
