package main

import "testing"

func TestListIndexAt(t *testing.T) {
	tests := []struct {
		name                      string
		y, cursor, total, visible int
		want                      int
	}{
		{"first row", listTopRow, 0, 5, 10, 0},
		{"third row", listTopRow + 2, 0, 5, 10, 2},
		{"above list", listTopRow - 1, 0, 5, 10, -1},
		{"below visible rows", listTopRow + 10, 0, 5, 10, -1},
		{"empty row past last item", listTopRow + 4, 0, 3, 10, -1},
		{"scrolled list maps through scrollStart", listTopRow, 10, 20, 4, 8},
		{"empty list", listTopRow, 0, 0, 10, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := listIndexAt(tt.y, tt.cursor, tt.total, tt.visible)
			if got != tt.want {
				t.Errorf("listIndexAt(%d,%d,%d,%d) got %d, want %d", tt.y, tt.cursor, tt.total, tt.visible, got, tt.want)
			}
		})
	}
}

func TestTabAtX(t *testing.T) {
	// "FragForge" (9) + 2 = 11; tabs are padded by one column each side.
	tests := []struct {
		name string
		x    int
		want int
	}{
		{"title area", 3, -1},
		{"demos tab start", 11, 0},
		{"demos tab end", 24, 0},
		{"streams tab", 26, 1},
		{"past tabs", 60, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tabAtX(tt.x); got != tt.want {
				t.Errorf("tabAtX(%d) got %d, want %d", tt.x, got, tt.want)
			}
		})
	}
}
