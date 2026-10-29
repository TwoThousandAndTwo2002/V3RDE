package rung

import (
	"regexp"
	"unicode"
)

var reTagAll = regexp.MustCompile(`<x\d+/>`)

func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}
