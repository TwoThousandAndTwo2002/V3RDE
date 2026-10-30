package i18n

import (
	"fmt"
	"strings"
	"testing"
)

// BatchGroups: the smallest screen, then the first concept (no concept first); equal parts of at most max.
func TestBatchGroups(t *testing.T) {
	g := GraphFile{Edges: []Edge{
		{"a", "screen:Big", "on_screen"}, {"a", "screen:Small", "on_screen"}, {"b", "screen:Big", "on_screen"},
		{"c", "screen:Big", "on_screen"}, {"x", "screen:Small", "on_screen"}, {"d", "concept:k", "uses"}, {"c", "concept:a", "uses"},
	}}
	ms := []Message{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}, {ID: "x"}}
	var got []string
	for _, gr := range BatchGroups(g, ms, 10) {
		var ids []string
		for _, m := range gr {
			ids = append(ids, m.ID)
		}
		got = append(got, strings.Join(ids, ","))
	}
	if s := strings.Join(got, " | "); s != "d | e | b,c | a,x" {
		t.Fatalf("groups: %s", s)
	}
	var many []Message
	for i := 0; i < 25; i++ {
		many = append(many, Message{ID: fmt.Sprint("m", i)})
	}
	var sizes []int
	for _, gr := range BatchGroups(GraphFile{}, many, 12) {
		sizes = append(sizes, len(gr))
	}
	if fmt.Sprint(sizes) != "[9 8 8]" {
		t.Fatalf("sizes %v", sizes)
	}
}
