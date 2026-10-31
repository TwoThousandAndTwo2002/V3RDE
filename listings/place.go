package listings

// X49e (#74, T9 2026-10-30, the approved "distance" mockup v2): the place of a
// listing and the distance to a receiver.
//
//   - The giver's place comes from the phone location (lat, lng) or, when
//     location is off, a US ZIP code (the center of its ZCTA, US Census
//     Gazetteer). The server keeps it rounded to a grid of 0.01 mi (T9: "lets
//     do .01 mi"), and never sends it: the app gets only distance_mi.
//   - The receiver sends its place with each list request (lat and lng, or a
//     zip). It is not stored.
//   - Distance: the haversine great-circle distance on a sphere of the mean
//     Earth radius (docs/sources/X49e.md).

import (
	_ "embed"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Place is a point in degrees (WGS 84 latitude and longitude).
type Place struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

const (
	// EarthMiles is the mean Earth radius, 6371.0088 km (IUGG), in miles of
	// 1609.344 m.
	EarthMiles = 6371.0088 / 1.609344
	// GridMiles is the grid the stored place is rounded to.
	GridMiles = 0.01
	// MaxMiles: a list with a place shows listings up to this distance (T9:
	// "25 miles is okay"; the gray "farther" part ends here).
	MaxMiles = 25.0
)

// ErrPlace is every bad place input; its text goes to the caller.
var ErrPlace = errors.New("send lat and lng, or a 5-digit US zip")

// ErrZip is a well-formed zip that the table does not have.
var ErrZip = errors.New("unknown zip code")

var zipRe = regexp.MustCompile(`^\d{5}$`)

// ParsePlace reads a place from form or query values: lat and lng together,
// or zip alone. All empty is (nil, nil): no place.
func ParsePlace(lat, lng, zip string) (*Place, error) {
	lat, lng, zip = strings.TrimSpace(lat), strings.TrimSpace(lng), strings.TrimSpace(zip)
	switch {
	case lat == "" && lng == "" && zip == "":
		return nil, nil
	case zip != "" && (lat != "" || lng != ""):
		return nil, ErrPlace
	case zip != "":
		if !zipRe.MatchString(zip) {
			return nil, ErrPlace
		}
		p, ok := ZipPlace(zip)
		if !ok {
			return nil, ErrZip
		}
		return &p, nil
	}
	a, err1 := strconv.ParseFloat(lat, 64)
	b, err2 := strconv.ParseFloat(lng, 64)
	if err1 != nil || err2 != nil || math.IsNaN(a) || math.IsNaN(b) || a < -90 || a > 90 || b < -180 || b > 180 {
		return nil, ErrPlace
	}
	return &Place{Lat: a, Lng: b}, nil
}

// Snap rounds p to the GridMiles grid: latitude in steps of GridMiles, and
// longitude in steps that are GridMiles long at that latitude. The point
// moves at most half a step on each axis.
func Snap(p Place) Place {
	step := GridMiles / EarthMiles * 180 / math.Pi // degrees of latitude in GridMiles
	lat := math.Round(p.Lat/step) * step
	lat = math.Max(-90, math.Min(90, lat))
	c := math.Cos(lat * math.Pi / 180)
	lng := p.Lng
	if c > 1e-6 { // at a pole every longitude is the same point
		ls := step / c
		lng = math.Round(p.Lng/ls) * ls
	}
	if lng > 180 {
		lng -= 360
	} else if lng < -180 {
		lng += 360
	}
	r6 := func(v float64) float64 { return math.Round(v*1e6) / 1e6 } // a short number in the data file
	return Place{Lat: r6(lat), Lng: r6(lng)}
}

// Miles is the haversine distance from a to b.
func Miles(a, b Place) float64 {
	rad := math.Pi / 180
	dLat := (b.Lat - a.Lat) * rad
	dLng := (b.Lng - a.Lng) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * EarthMiles * math.Asin(math.Min(1, math.Sqrt(h)))
}

// ShownMiles is the step of distance_mi: the app shows one decimal, and a
// finer value would let a caller find the giver's point from a few made-up
// places (X49e review). The stored place stays at GridMiles.
const ShownMiles = 0.1

// roundMiles is a distance as the app gets it: to ShownMiles (one decimal).
func roundMiles(d float64) float64 { return math.Round(d/ShownMiles) / (1 / ShownMiles) }

// zcta.csv: "zip,lat,lng" per line, made from the US Census 2024 Gazetteer
// ZCTA file (tools/zcta_table.py; docs/sources/X49e.md).
//
//go:embed zcta.csv
var zctaCSV string

var (
	zipOnce  sync.Once
	zipTable map[string]Place
)

// ZipPlace is the center of a ZIP code (its ZCTA).
func ZipPlace(zip string) (Place, bool) {
	zipOnce.Do(func() {
		zipTable = make(map[string]Place, 34000)
		for _, line := range strings.Split(zctaCSV, "\n") {
			f := strings.Split(strings.TrimSpace(line), ",")
			if len(f) != 3 || !zipRe.MatchString(f[0]) {
				continue // the header line, or an empty line
			}
			a, err1 := strconv.ParseFloat(f[1], 64)
			b, err2 := strconv.ParseFloat(f[2], 64)
			if err1 == nil && err2 == nil {
				zipTable[f[0]] = Place{Lat: a, Lng: b}
			}
		}
	})
	p, ok := zipTable[zip]
	return p, ok
}

// ZipCount is the number of ZIP codes in the table (the health answer).
func ZipCount() int {
	ZipPlace("00000")
	return len(zipTable)
}
