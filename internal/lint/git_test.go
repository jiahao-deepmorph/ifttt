package lint

import "testing"

func TestLineMappingAndHunks(t *testing.T) {
	c := parseHunks([]byte("@@ -0,0 +1,2 @@\n+prefix\n+prefix\n@@ -4,2 +6 @@\n-old\n-old\n+new\n@@ -9 +10,0 @@\n-removed\n"))
	for _, tt := range []struct {
		old, new int
		survives bool
	}{
		{1, 3, true}, {3, 5, true}, {4, 0, false}, {5, 0, false}, {6, 7, true}, {8, 9, true}, {9, 0, false}, {10, 10, true},
	} {
		line, ok := c.mapLine(tt.old)
		if line != tt.new || ok != tt.survives {
			t.Errorf("old %d: got %d,%v want %d,%v", tt.old, line, ok, tt.new, tt.survives)
		}
	}
	if len(c.hunks) != 3 {
		t.Fatal(c)
	}
}

func TestBodyIntersectionBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name   string
		region Region
		span   span
		want   bool
	}{
		{"opening marker", Region{Open: 3, Close: 6}, span{3, 1}, false},
		{"first body line", Region{Open: 3, Close: 6}, span{4, 1}, true},
		{"last body line", Region{Open: 3, Close: 6}, span{5, 1}, true},
		{"closing marker", Region{Open: 3, Close: 6}, span{6, 1}, false},
		{"continuations", Region{Open: 3, Close: 6, End: 9}, span{7, 3}, false},
		{"zero length at body", Region{Open: 3, Close: 6}, span{4, 0}, false},
		{"ends at opening marker", Region{Open: 3, Close: 6}, span{1, 3}, false},
		{"spans whole body", Region{Open: 3, Close: 6}, span{1, 10}, true},
		{"spans empty region", Region{Open: 3, Close: 4}, span{1, 10}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.span.intersects(tt.region); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}
