package mask

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

var keep = []string{"V3RDE", "WhatsApp"}

// Placeholders and keep names become numbered tags in text order; a keep name
// inside a longer word stays text.
func TestMask(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Item {n}: “{from}” to “{to}”", "Item <x1/>: “<x2/>” to “<x3/>”"},
		{`Hi! I grabbed "{title}" on V3RDE ({servings}).`, `Hi! I grabbed "<x1/>" on <x2/> (<x3/>).`},
		{"Open WhatsApp", "Open <x1/>"},
		{"V3RDEX and WhatsAppy", "V3RDEX and WhatsAppy"},
		{"No tags here", "No tags here"},
	} {
		m, err := Mask(c.in, keep)
		if err != nil || m.Text != c.want {
			t.Errorf("Mask(%q) = %q, %v; want %q", c.in, m.Text, err, c.want)
		}
	}
	if _, err := Mask("a <x1/> b", keep); err == nil {
		t.Error("a source with a tag must fail")
	}
}

// Restore works in every target script, with the tags in a new order and with
// spaces in a tag; Arabic is right to left.
func TestRestoreScripts(t *testing.T) {
	m, _ := Mask("Item {n}: “{from}” to “{to}”", keep)
	for lang, c := range map[string]struct{ in, want string }{
		"es-419":  {"Artículo <x1/>: de «<x2/>» a «<x3/>»", "Artículo {n}: de «{from}» a «{to}»"},
		"zh-Hans": {"第<x1/>项：从“<x2/>”改为“<x3/>”", "第{n}项：从“{from}”改为“{to}”"},
		"hi":      {"आइटम <x1/>: “<x2/>” से “<x3/>”", "आइटम {n}: “{from}” से “{to}”"},
		"ar":      {"العنصر <x1/>: من «<x2/>» إلى «<x3 />»", "العنصر {n}: من «{from}» إلى «{to}»"},
		"order":   {"<x3/> ← <x2/> (<x1/>)", "{to} ← {from} ({n})"},
	} {
		got, err := m.Restore(c.in)
		if err != nil || got != c.want {
			t.Errorf("%s: Restore = %q, %v; want %q", lang, got, err, c.want)
		}
	}
}

// A lost, doubled or unknown tag, or a bare placeholder, is an error.
func TestRestoreFails(t *testing.T) {
	m, _ := Mask("Remove item {n}: {title}", keep)
	for in, want := range map[string]string{
		"Quitar artículo <x1/>":            "<x2/> 0 times",
		"Quitar <x1/> <x1/>: <x2/>":        "<x1/> 2 times",
		"Quitar <x1/>: <x2/> <x3/>":        "unknown tag <x3/>",
		"Quitar artículo {n}: <x1/> <x2/>": "bare placeholder {n}",
	} {
		_, err := m.Restore(in)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Restore(%q): %v; want %q", in, err, want)
		}
	}
}

// Every message of en.json masks and restores to itself, with the keep names of
// languages.json.
func TestRoundTripEnJSON(t *testing.T) {
	var en struct {
		Messages []struct{ ID, Text string }
	}
	var langs struct{ Keep []string }
	for p, v := range map[string]any{"../en.json": &en, "../languages.json": &langs} {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, v); err != nil {
			t.Fatal(err)
		}
	}
	if len(en.Messages) == 0 || len(langs.Keep) == 0 {
		t.Fatal("no messages or no keep names")
	}
	for _, msg := range en.Messages {
		m, err := Mask(msg.Text, langs.Keep)
		if err != nil {
			t.Fatalf("%s: %v", msg.ID, err)
		}
		back, err := m.Restore(m.Text)
		if err != nil || back != msg.Text {
			t.Errorf("%s: %q -> %q -> %q, %v", msg.ID, msg.Text, m.Text, back, err)
		}
	}
}
