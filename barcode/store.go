package barcode

// Row is one catalog row: table p (gtin14 TEXT PRIMARY KEY, title,
// servings, weight_kg) WITHOUT ROWID.
type Row struct {
	Title    string
	Servings float64
	WeightKg float64
}

// Store answers a canonical GTIN-14: SQLite in the service (store_sqlite.go,
// build tag sqlite), a map in the tests.
type Store interface {
	Get(gtin14 string) (Row, bool, error)
	Rows() int
}

// MapStore is a Store in memory.
type MapStore map[string]Row

// Get is the row of g14.
func (m MapStore) Get(g14 string) (Row, bool, error) {
	r, ok := m[g14]
	return r, ok, nil
}

// Rows is the number of rows.
func (m MapStore) Rows() int { return len(m) }

// ExpectedRows is the row count of the pinned catalog. A file that opens
// with fewer rows is a cut copy, and it would answer "not found" for the
// part it lacks, as if the catalog did not hold those products.
const ExpectedRows = 374760

// Lookup: the canonical key, one query, then Complete. invalid is set for a
// code that has no key; found is false for a valid code the catalog does
// not hold; err is set only when the store failed.
func Lookup(s Store, raw string) (p Product, g14 string, found bool, invalid error, err error) {
	if g14, invalid = Canonical(raw); invalid != nil {
		return Product{}, "", false, invalid, nil
	}
	r, ok, err := s.Get(g14)
	if err != nil || !ok {
		return Product{}, g14, false, nil, err
	}
	p = Complete(Product{Barcode: g14, Title: r.Title, NumberOfServing: r.Servings, Weight: r.WeightKg, Source: SourceCatalog})
	return p, g14, true, nil, nil
}
