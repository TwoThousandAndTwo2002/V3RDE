package inference

// X32a: the one route calls the internal services that /api/scan and
// /api/inventory called before (router/auth.go, router/router.go,
// router/inventory.go, unchanged rules): auth (who owns the session
// cookie), barcode (the catalog), listings (the caller's live listings).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SessionCookie is the app's session cookie (auth/auth.go).
const SessionCookie = "morpheus_session"

// User is whoever auth's /api/auth/me says owns the session cookie.
type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// Authenticator finds the signed-in caller.
type Authenticator interface {
	Who(r *http.Request) (User, bool)
}

// AuthClient asks the auth service.
type AuthClient struct {
	URL  string // http://auth:8081
	HTTP *http.Client
}

func (a *AuthClient) Who(r *http.Request) (User, bool) {
	ck, err := r.Cookie(SessionCookie)
	if err != nil || ck.Value == "" {
		return User{}, false
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(a.URL, "/")+"/api/auth/me", nil)
	if err != nil {
		return User{}, false
	}
	req.AddCookie(ck)
	hc := a.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return User{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return User{}, false
	}
	var u User
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&u); err != nil || u.ID == "" {
		return User{}, false
	}
	return u, true
}

// CatalogProduct is a barcode hit (barcode/handler.go).
type CatalogProduct struct {
	Barcode         string  `json:"barcode"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	IsNonVeg        int     `json:"is_non_veg"`
	NumberOfServing float64 `json:"number_of_serving"`
	ServingType     string  `json:"serving_type"`
	MealCondition   string  `json:"meal_condition"`
	Weight          float64 `json:"weight"`
	MealCost        float64 `json:"meal_cost"`
}

type barcodeAnswer struct {
	Outcome    string          `json:"outcome"`
	GTIN14     string          `json:"gtin14"` // N1: the key of the stock photo
	Admissible bool            `json:"admissible"`
	Product    *CatalogProduct `json:"product"`
}

// lookupBarcode is GET <barcode>/v1/barcode/<code>. An error answers like a
// miss (the prior product's EmptyCatalog).
func lookupBarcode(r *http.Request, hc *http.Client, base, trace, code string) (barcodeAnswer, int, error) {
	var out barcodeAnswer
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(base, "/")+"/v1/barcode/"+url.PathEscape(code), nil)
	if err != nil {
		return out, 0, err
	}
	req.Header.Set("X-Trace-Id", trace)
	resp, err := hc.Do(req)
	if err != nil {
		return out, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, resp.StatusCode, fmt.Errorf("status %d", resp.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out)
	return out, resp.StatusCode, err
}

// ListedRef is one of the caller's live listings.
type ListedRef struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	NumberOfServing int    `json:"number_of_serving"`
	ExpiresAt       string `json:"expires_at"`
}

// liveListings is GET <listings>/api/listings/mine with the caller's cookie,
// filtered to active listings that have not expired (router/inventory.go).
func liveListings(r *http.Request, hc *http.Client, base string, now time.Time) ([]ListedRef, error) {
	ck, err := r.Cookie(SessionCookie)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, strings.TrimRight(base, "/")+"/api/listings/mine", nil)
	if err != nil {
		return nil, err
	}
	req.AddCookie(ck)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var body struct {
		Items []struct {
			ListedRef
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, err
	}
	var out []ListedRef
	for _, l := range body.Items {
		exp, err := time.Parse(time.RFC3339Nano, l.ExpiresAt)
		if l.Status == "active" && err == nil && now.Before(exp) {
			out = append(out, l.ListedRef)
		}
	}
	return out, nil
}

// listedByName maps normName(title) to the newest live listing.
func listedByName(live []ListedRef) map[string]ListedRef {
	m := map[string]ListedRef{}
	for _, l := range live { // newest first: the newest listing wins
		if n := normName(l.Title); n != "" {
			if _, seen := m[n]; !seen {
				m[n] = l
			}
		}
	}
	return m
}
