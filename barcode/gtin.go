// Package barcode is the V3RDE barcode lookup: a scanned code in, the
// catalog's product out (title, servings, weight), as the eight-field
// listing contract.
//
// V3RDE #27 (Oct 28 2026): this package is written new for V3RDE. The
// route, the JSON of the answer (the inference service reads it), the
// outcomes, the events line and the catalog it reads are kept the same as
// the barcode service the app used before the Hackathon.
//
// The catalog is keyed on the canonical GTIN-14: every way to write one
// product (GTIN-8, UPC-A/GTIN-12, EAN-13, GTIN-14) becomes one key, so one
// query on the canonical form finds it.
package barcode

import "fmt"

// Normalize keeps the ASCII digits 0-9 of raw and drops everything else
// (spaces, dashes, other scripts' digits). It never adds a zero.
func Normalize(raw string) string {
	out := make([]byte, 0, len(raw))
	for _, c := range []byte(raw) {
		if '0' <= c && c <= '9' {
			out = append(out, c)
		}
	}
	return string(out)
}

// CheckDigitOK applies the GS1 check digit: from the digit left of the check
// digit, going left, the weights are 3, 1, 3, 1, ...; the check digit makes
// the sum a multiple of 10
// (https://gs1.org/how-calculate-check-digit-manually).
func CheckDigitOK(code string) bool {
	d := Normalize(code)
	if len(d) < 8 || len(d) > 14 {
		return false
	}
	sum := 0
	for i, n := len(d)-2, 0; i >= 0; i, n = i-1, n+1 {
		w := 3
		if n%2 == 1 {
			w = 1
		}
		sum += int(d[i]-'0') * w
	}
	return (10-sum%10)%10 == int(d[len(d)-1]-'0')
}

// Canonical is the GTIN-14 of a valid GTIN-8/12/13/14: zeros on the left
// (they do not change the check digit, because their weight times 0 is 0).
// The error says why a code has no key.
func Canonical(raw string) (string, error) {
	d := Normalize(raw)
	switch {
	case d == "":
		return "", fmt.Errorf("no digits")
	case len(d) != 8 && len(d) != 12 && len(d) != 13 && len(d) != 14:
		return "", fmt.Errorf("%d digits; want GTIN-8/12/13/14", len(d))
	case !CheckDigitOK(d):
		return "", fmt.Errorf("fails its mod-10 check digit")
	}
	return fmt.Sprintf("%014s", d), nil
}

// SameProduct: both codes are valid and have one canonical key.
func SameProduct(a, b string) bool {
	ca, ea := Canonical(a)
	cb, eb := Canonical(b)
	return ea == nil && eb == nil && ca == cb
}

// EquivalentKeys is every width under which a store keyed on the written
// form could hold the code, the scanned form first: a 12-digit code also as
// 13 and 14 digits; a 13-digit code without its leading zero and as 14; a
// 14-digit code without one or two leading zeros only (a case pack with
// another first digit is another product).
func EquivalentKeys(raw string) []string {
	d := Normalize(raw)
	if d == "" {
		return nil
	}
	keys := []string{d}
	add := func(k string) {
		for _, have := range keys {
			if have == k {
				return
			}
		}
		keys = append(keys, k)
	}
	switch len(d) {
	case 12:
		add("0" + d)
		add("00" + d)
	case 13:
		if d[0] == '0' {
			add(d[1:])
		}
		add("0" + d)
	case 14:
		if d[0] == '0' {
			add(d[1:])
			if d[1] == '0' {
				add(d[2:])
			}
		}
	}
	return keys
}
