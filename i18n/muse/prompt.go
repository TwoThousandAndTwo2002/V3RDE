package muse

import "fmt"

// PromptPlain names the prompt of L4b (no context).
const PromptPlain = "plain"

// PromptContext names the prompt of L4h: the text with its context from the text graph.
const PromptContext = "context-v1"

// Prompt makes the system and user messages that translate one masked message.
// lang is the English name and tag of the target (Spanish (Latin America), es-419);
// id is the message id, the only context of a short UI text.
func Prompt(langName, langCode, id, masked string) (system, user string) {
	return PromptWith(langName, langCode, id, masked, "")
}

// PromptWith is Prompt with a context block (L4h): what the text does, its
// values, its pairs, the texts near it, and earlier translations of the same idea.
func PromptWith(langName, langCode, id, masked, about string) (system, user string) {
	system = fmt.Sprintf("You translate the user interface text of a mobile app for sharing food, from English to %s (%s).\n"+
		"Rules:\n"+
		"1. Reply with the translation only. No notes, no quotes around it, no explanation.\n"+
		"2. Each tag like <x1/> stands for a name or a value. Keep every tag exactly as it is, once each. Move a tag if the grammar of %s needs it.\n"+
		"3. Use the words and the tone that mobile apps in %s use. Keep the text short, like the English.\n"+
		"4. Keep the punctuation that the text needs (a question stays a question; an ellipsis stays).",
		langName, langCode, langName, langName)
	if about != "" {
		system += "\n5. Read the context. It says what kind of text this is (a button needs a verb, a title often a noun phrase), what it does for the user, and the texts near it. " +
			"The translation must fit that action and must not read like a near text with another action (for example, \"Log out\" must not read like \"sign in\"). " +
			"Use the same word as the earlier translations of the same idea, unless the context needs another word."
		user = fmt.Sprintf("Message id: %s\nContext:\n%s\nText:\n%s", id, about, masked)
		return system, user
	}
	user = fmt.Sprintf("Message id: %s\nText:\n%s", id, masked)
	return system, user
}
