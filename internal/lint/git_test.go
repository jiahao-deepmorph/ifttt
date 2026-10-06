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
