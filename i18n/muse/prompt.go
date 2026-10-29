package muse

import "fmt"

// Prompt makes the system and user messages that translate one masked message.
// lang is the English name and tag of the target (Spanish (Latin America), es-419);
// id is the message id, the only context of a short UI text.
func Prompt(langName, langCode, id, masked string) (system, user string) {
	system = fmt.Sprintf("You translate the user interface text of a mobile app for sharing food, from English to %s (%s).\n"+
		"Rules:\n"+
		"1. Reply with the translation only. No notes, no quotes around it, no explanation.\n"+
		"2. Each tag like <x1/> stands for a name or a value. Keep every tag exactly as it is, once each. Move a tag if the grammar of %s needs it.\n"+
		"3. Use the words and the tone that mobile apps in %s use. Keep the text short, like the English.\n"+
		"4. Keep the punctuation that the text needs (a question stays a question; an ellipsis stays).",
		langName, langCode, langName, langName)
	user = fmt.Sprintf("Message id: %s\nText:\n%s", id, masked)
	return system, user
}
