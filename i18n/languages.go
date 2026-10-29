package i18n

// Languages is i18n/languages.json: the source language, the target
// languages (es-419 first), and the names that stay as they are in every language.
type Languages struct {
	Source    string     `json:"source"`
	Languages []Language `json:"languages"`
	Keep      []string   `json:"keep"`
}

// Language is one language of the app.
type Language struct {
	Code   string `json:"code"`   // BCP 47 tag
	Name   string `json:"name"`   // English name
	Native string `json:"native"` // its name in itself
	Script string `json:"script"` // ISO 15924 code
	Dir    string `json:"dir"`    // ltr or rtl
	Role   string `json:"role"`   // source, first (translated first) or target
}
