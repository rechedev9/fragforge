package streamclips

import (
	"image"
	"math"
	"sort"
)

const (
	killfeedSearchPadding    = 8
	killfeedMinStrokeLength  = 40
	killfeedStrokeGapBridge  = 8
	killfeedMinStrokeDensity = 0.5
	killfeedStrokeYSlop      = 2
	killfeedMaxStrokeRuns    = 6
	killfeedEdgeSlop         = 3
	killfeedMinRowHeight     = 20
	killfeedLooseSearch      = 6
	// These shape thresholds mirror internal/editor/killfeed_probe.go. Keep the
	// stream detector independent from the editor package boundary.
	killfeedMinRowAspect = 2
	killfeedMaxRowFill   = 0.5
	// Limit loose-edge growth so an overlapping neighboring notice cannot
	// stretch this row to the full six-pixel search boundary.
	killfeedLooseEdgeReach = 1
	killfeedRowMargin      = 1
)

// NoticeRow is the source-pixel crop of one highlighted CS2 kill notice.
type NoticeRow struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// DetectNoticeRows finds the saturated-red top and bottom stroke pair of each
// highlighted CS2 kill notice. Pairing horizontal strokes instead of connected
// components keeps staggered notices separate even when their border rings
// touch.
func DetectNoticeRows(frame image.Image, hint *CropRect) []NoticeRow {
	if frame == nil {
		return nil
	}
	bounds := frame.Bounds()
	region := noticeSearchRegion(bounds, hint)
	if region.Empty() {
		return nil
	}

	// Compression can fragment the saturated ring so only the loose mask pairs
	// it. A bright loose-red notice interior can instead collapse that mask into
	// one mega-stroke while its strict ring still pairs. Run both passes because
	// one frame can contain both styles.
	looseBoxes := detectNoticeBoxes(frame, region, bounds.Dy(), 120, 70)
	strictBoxes := detectNoticeBoxes(frame, region, bounds.Dy(), 150, 55)
	boxes := make([]image.Rectangle, 0, len(looseBoxes)+len(strictBoxes))
	boxes = append(boxes, looseBoxes...)
	boxes = append(boxes, strictBoxes...)
	boxes = dedupeOverlappingNoticeBoxes(boxes)
	if len(boxes) == 0 {
		return nil
	}

	for i := range boxes {
		search := boxes[i].Inset(-killfeedLooseSearch).Intersect(bounds)
		if edge, ok := looseRedBounds(frame, search); ok {
			edgeLimit := boxes[i].Inset(-killfeedLooseEdgeReach).Intersect(bounds)
			if edge = edge.Intersect(edgeLimit); !edge.Empty() {
				boxes[i] = boxes[i].Union(edge)
			}
		}
		boxes[i] = boxes[i].Inset(-killfeedRowMargin).Intersect(region)
	}
	boxes = dedupeContainedNoticeBoxes(boxes)
	sort.Slice(boxes, func(i, j int) bool {
		if boxes[i].Min.Y != boxes[j].Min.Y {
			return boxes[i].Min.Y < boxes[j].Min.Y
		}
		return boxes[i].Min.X < boxes[j].Min.X
	})

	rows := make([]NoticeRow, len(boxes))
	for i, box := range boxes {
		rows[i] = NoticeRow{X: box.Min.X, Y: box.Min.Y, Width: box.Dx(), Height: box.Dy()}
	}
	return rows
}

func noticeSearchRegion(bounds image.Rectangle, hint *CropRect) image.Rectangle {
	if hint == nil {
		return image.Rect(
			bounds.Min.X+bounds.Dx()*3/5,
			bounds.Min.Y,
			bounds.Max.X,
			bounds.Min.Y+bounds.Dy()*3/10,
		)
	}

	minX := bounds.Min.X + int(math.Floor(hint.X*float64(bounds.Dx())))
	minY := bounds.Min.Y + int(math.Floor(hint.Y*float64(bounds.Dy())))
	maxX := bounds.Min.X + int(math.Ceil((hint.X+hint.Width)*float64(bounds.Dx())))
	maxY := bounds.Min.Y + int(math.Ceil((hint.Y+hint.Height)*float64(bounds.Dy())))
	return image.Rect(
		minX-killfeedSearchPadding,
		minY-killfeedSearchPadding,
		maxX+killfeedSearchPadding,
		maxY+killfeedSearchPadding,
	).Intersect(bounds)
}

func redMask(frame image.Image, region image.Rectangle, minRed, maxGreenBlue uint32) []bool {
	width, height := region.Dx(), region.Dy()
	mask := make([]bool, width*height)
	for dy := range height {
		for dx := range width {
			r, g, b, _ := frame.At(region.Min.X+dx, region.Min.Y+dy).RGBA()
			mask[dy*width+dx] = r>>8 > minRed && g>>8 < maxGreenBlue && b>>8 < maxGreenBlue
		}
	}
	return mask
}

type horizontalRun struct {
	y    int
	xMin int
	xMax int
}

func horizontalRedRuns(mask []bool, region image.Rectangle) []horizontalRun {
	width, height := region.Dx(), region.Dy()
	var runs []horizontalRun
	for dy := range height {
		runStart := -1
		lastRed := -1
		redCount := 0
		for dx := range width {
			if !mask[dy*width+dx] {
				continue
			}
			if runStart >= 0 && dx-lastRed-1 > killfeedStrokeGapBridge {
				runs = appendHorizontalRun(runs, region.Min.Y+dy, region.Min.X, runStart, lastRed+1, redCount)
				runStart = -1
				redCount = 0
			}
			if runStart < 0 {
				runStart = dx
			}
			lastRed = dx
			redCount++
		}
		if runStart >= 0 {
			runs = appendHorizontalRun(runs, region.Min.Y+dy, region.Min.X, runStart, lastRed+1, redCount)
		}
	}
	return runs
}

func appendHorizontalRun(runs []horizontalRun, y, xOffset, start, end, redCount int) []horizontalRun {
	extent := end - start
	if extent < killfeedMinStrokeLength || float64(redCount)/float64(extent) < killfeedMinStrokeDensity {
		return runs
	}
	return append(runs, horizontalRun{y: y, xMin: xOffset + start, xMax: xOffset + end})
}

type horizontalStroke struct {
	y    int
	xMin int
	xMax int
	runs int
}

type strokeAccumulator struct {
	ySum     int
	xMinSum  int
	xMaxSum  int
	count    int
	lastY    int
	lastXMin int
	lastXMax int
}

func mergeHorizontalRuns(runs []horizontalRun) []horizontalStroke {
	var groups []strokeAccumulator
	for _, run := range runs {
		best := -1
		bestOverlap := 0
		for i := range groups {
			group := &groups[i]
			if run.y-group.lastY > killfeedStrokeYSlop {
				continue
			}
			overlap := intervalOverlap(run.xMin, run.xMax, group.lastXMin, group.lastXMax)
			wider := max(run.xMax-run.xMin, group.lastXMax-group.lastXMin)
			// Compare against the wider run. Using the shorter run would merge a
			// narrow notice stroke into a staggered wider notice that contains it.
			if overlap*100 < 80*wider || overlap <= bestOverlap {
				continue
			}
			best = i
			bestOverlap = overlap
		}
		if best < 0 {
			groups = append(groups, strokeAccumulator{
				ySum: run.y, xMinSum: run.xMin, xMaxSum: run.xMax, count: 1,
				lastY: run.y, lastXMin: run.xMin, lastXMax: run.xMax,
			})
			continue
		}
		group := &groups[best]
		group.ySum += run.y
		group.xMinSum += run.xMin
		group.xMaxSum += run.xMax
		group.count++
		group.lastY = run.y
		group.lastXMin = run.xMin
		group.lastXMax = run.xMax
	}

	strokes := make([]horizontalStroke, len(groups))
	for i, group := range groups {
		strokes[i] = horizontalStroke{
			y:    roundedMean(group.ySum, group.count),
			xMin: roundedMean(group.xMinSum, group.count),
			xMax: roundedMean(group.xMaxSum, group.count),
			runs: group.count,
		}
	}
	sort.Slice(strokes, func(i, j int) bool {
		if strokes[i].y != strokes[j].y {
			return strokes[i].y < strokes[j].y
		}
		return strokes[i].xMin < strokes[j].xMin
	})
	return strokes
}

func intervalOverlap(aMin, aMax, bMin, bMax int) int {
	return max(0, min(aMax, bMax)-max(aMin, bMin))
}

func roundedMean(sum, count int) int {
	return (sum + count/2) / count
}

func detectNoticeBoxes(frame image.Image, region image.Rectangle, frameHeight int, minRed, maxGreenBlue uint32) []image.Rectangle {
	mask := redMask(frame, region, minRed, maxGreenBlue)
	strokes := mergeHorizontalRuns(horizontalRedRuns(mask, region))
	return filterNoticeBoxes(frame, pairNoticeStrokes(strokes, frameHeight), frameHeight)
}

func filterNoticeBoxes(frame image.Image, boxes []image.Rectangle, frameHeight int) []image.Rectangle {
	filtered := boxes[:0]
	for _, box := range boxes {
		if box.Dx() < killfeedMinRowAspect*box.Dy() || box.Dy() > frameHeight/12 {
			continue
		}
		strictPixels := 0
		for y := box.Min.Y; y < box.Max.Y; y++ {
			for x := box.Min.X; x < box.Max.X; x++ {
				r, g, b, _ := frame.At(x, y).RGBA()
				if r>>8 > 150 && g>>8 < 55 && b>>8 < 55 {
					strictPixels++
				}
			}
		}
		if float64(strictPixels)/float64(rectangleArea(box)) > killfeedMaxRowFill {
			continue
		}
		filtered = append(filtered, box)
	}
	return filtered
}

func pairNoticeStrokes(strokes []horizontalStroke, frameHeight int) []image.Rectangle {
	usedAsTop := make([]bool, len(strokes))
	usedAsBottom := make([]bool, len(strokes))
	maxHeight := frameHeight / 12
	idealHeight := max(killfeedMinRowHeight, min(maxHeight, frameHeight/27))
	var boxes []image.Rectangle
	for i, top := range strokes {
		if top.runs > killfeedMaxStrokeRuns {
			continue
		}
		// When two equal-width notices overlap, their adjacent bottom/top
		// borders can merge into one four-run stroke. Such a stroke may close
		// the earlier notice and open the next; ordinary two-run borders keep
		// exactly one role so separated rows cannot be paired through the gap.
		sharedBoundary := top.runs >= 4
		if usedAsTop[i] || (usedAsBottom[i] && !sharedBoundary) {
			continue
		}
		best := -1
		bestHeightDelta := maxHeight + 1
		for j := i + 1; j < len(strokes); j++ {
			if strokes[j].runs > killfeedMaxStrokeRuns {
				continue
			}
			bottomSharedBoundary := strokes[j].runs >= 4
			if usedAsBottom[j] || (usedAsTop[j] && !bottomSharedBoundary) {
				continue
			}
			bottom := strokes[j]
			gap := bottom.y - top.y
			if gap < killfeedMinRowHeight {
				continue
			}
			if gap > maxHeight {
				break
			}
			if abs(top.xMin-bottom.xMin) > killfeedEdgeSlop || abs(top.xMax-bottom.xMax) > killfeedEdgeSlop {
				continue
			}
			heightDelta := abs(gap - idealHeight)
			if heightDelta < bestHeightDelta {
				best = j
				bestHeightDelta = heightDelta
			}
		}
		if best >= 0 {
			usedAsTop[i] = true
			usedAsBottom[best] = true
			bottom := strokes[best]
			boxes = append(boxes, image.Rect(top.xMin, top.y, top.xMax, bottom.y))
		}
	}
	return boxes
}

func dedupeOverlappingNoticeBoxes(boxes []image.Rectangle) []image.Rectangle {
	result := make([]image.Rectangle, 0, len(boxes))
	for _, box := range boxes {
		candidate := box
		kept := result[:0]
		for _, existing := range result {
			if rectangleIoU(existing, candidate) <= 0.5 {
				kept = append(kept, existing)
				continue
			}
			if rectangleArea(existing) >= rectangleArea(candidate) {
				candidate = existing
			}
		}
		result = append(kept, candidate)
	}
	return result
}

func rectangleIoU(a, b image.Rectangle) float64 {
	intersectionArea := rectangleArea(a.Intersect(b))
	if intersectionArea == 0 {
		return 0
	}
	unionArea := rectangleArea(a) + rectangleArea(b) - intersectionArea
	return float64(intersectionArea) / float64(unionArea)
}

func rectangleArea(rect image.Rectangle) int {
	if rect.Empty() {
		return 0
	}
	return rect.Dx() * rect.Dy()
}

func looseRedBounds(frame image.Image, region image.Rectangle) (image.Rectangle, bool) {
	found := image.Rectangle{}
	ok := false
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			r, g, b, _ := frame.At(x, y).RGBA()
			if r>>8 <= 120 || g>>8 >= 70 || b>>8 >= 70 {
				continue
			}
			pixel := image.Rect(x, y, x+1, y+1)
			if !ok {
				found = pixel
				ok = true
			} else {
				found = found.Union(pixel)
			}
		}
	}
	return found, ok
}

func dedupeContainedNoticeBoxes(boxes []image.Rectangle) []image.Rectangle {
	result := make([]image.Rectangle, 0, len(boxes))
	for i, box := range boxes {
		contained := false
		for j, other := range boxes {
			if i == j || !rectangleContains(other, box) {
				continue
			}
			if other != box || j < i {
				contained = true
				break
			}
		}
		if !contained {
			result = append(result, box)
		}
	}
	return result
}

func rectangleContains(outer, inner image.Rectangle) bool {
	return outer.Min.X <= inner.Min.X && outer.Min.Y <= inner.Min.Y &&
		outer.Max.X >= inner.Max.X && outer.Max.Y >= inner.Max.Y
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
