package muse

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PromptBatch names the prompt of L4k: one call translates a group of texts
// (one screen, or the texts of one concept), each with its context.
const PromptBatch = "batch-v1"

// BatchItem is one text of a batch: its id, its context block and its masked text.
type BatchItem struct {
	ID      string `json:"id"`
	Context string `json:"context,omitempty"`
	Text    string `json:"text"`
}

// PromptBatchWith makes the system and user messages that translate a group of
// masked texts in one call. The answer is one JSON object: id -> translation.
func PromptBatchWith(langName, langCode string, items []BatchItem) (system, user string, err error) {
	system = fmt.Sprintf("You translate the user interface texts of a mobile app for sharing food, from English to %s (%s).\n"+
		"You get a JSON list of texts that the app shows together (one screen, or the texts of one idea). Each has an id, its context and its text.\n"+
		"Rules:\n"+
		"1. Reply with one JSON object only: each key is an id from the list, each value is the translation of its text. No other text, no notes.\n"+
		"2. Translate every text of the list, once.\n"+
		"3. Each tag like <x1/> stands for a name or a value. Keep every tag of a text exactly as it is, once each, in the translation of that text. Move a tag if the grammar of %s needs it.\n"+
		"4. Use the words and the tone that mobile apps in %s use. Keep each text short, like the English. Keep the punctuation that the text needs.\n"+
		"5. Read each context: a button needs the action form, a title often a noun phrase. A translation must fit what the text does and must not read like a near text with another action.\n"+
		"6. Use one word for one idea in all the texts of the list, and the same form of address to the user in all of them.",
		langName, langCode, langName, langName)
	b, err := json.MarshalIndent(items, "", " ")
	if err != nil {
		return "", "", err
	}
	user = "Texts:\n" + string(b)
	return system, user, nil
}

// ParseBatch reads the JSON object of a batch answer (the first "{" to the
// last "}", so a fenced answer is also read).
func ParseBatch(raw string) (map[string]string, error) {
	a, b := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if a < 0 || b < a {
		return nil, fmt.Errorf("batch: no JSON object in the answer")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw[a:b+1]), &m); err != nil {
		return nil, fmt.Errorf("batch: %v", err)
	}
	return m, nil
}
