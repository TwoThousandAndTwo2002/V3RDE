package inference

// Prices in USD, from dev.meta.ai/docs/pricing-rate-limits (READ 2026-10-26).
// Muse Spark 1.3 standard mode is billed per token; reasoning tokens are
// counted inside output_tokens. SAM 3.1 is billed per image request. A call
// that fails before the API answers 2xx is counted as free here.
const (
	PriceMuseInputPerM  = 1.25
	PriceMuseOutputPerM = 4.25
	PriceMuseCachedPerM = 0.15
	PriceSAMPerImage    = 2.50 / 1000
	PricesSource        = "dev.meta.ai/docs/pricing-rate-limits (2026-10-26)"
)

// Cost is this call's price in USD.
func (c *Call) Cost() float64 {
	if c.Status/100 != 2 {
		return 0
	}
	if c.Model == DefaultSAM {
		return PriceSAMPerImage
	}
	fresh := c.Usage.InputTokens - c.CachedTokens
	if fresh < 0 {
		fresh = 0
	}
	return (float64(fresh)*PriceMuseInputPerM + float64(c.CachedTokens)*PriceMuseCachedPerM +
		float64(c.Usage.OutputTokens)*PriceMuseOutputPerM) / 1e6
}

// CostUSD is the photo's total: every SAM and Muse call it made.
func (inv *Inventory) CostUSD() float64 {
	s := 0.0
	for _, c := range inv.Calls {
		s += c.Cost()
	}
	return s
}

// Listings is the number of listings the answer creates (one per group).
func (inv *Inventory) Listings() int { return len(inv.Items) }

// Units is the number of physical items (boxes) the answer covers.
func (inv *Inventory) Units() int {
	n := 0
	for _, it := range inv.Items {
		n += it.Count
	}
	return n
}

// SpentUSD is what this run actually paid: CostUSD without probe-cached SAM
// answers.
func (inv *Inventory) SpentUSD() float64 {
	s := 0.0
	for _, c := range inv.Calls {
		if !c.Cached {
			s += c.Cost()
		}
	}
	return s
}

// Counted is every physical item found: the named units plus the boxes
// counted but not named (over the cap, or too small to name).
func (inv *Inventory) Counted() int { return inv.Units() + inv.Unnamed + inv.Small }
