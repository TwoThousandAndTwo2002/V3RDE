package inference

// X44b (F1, "no visible waits"): what the engine has found so far, for the
// scan job (scanjob.go). The engine reports through the request context, so
// a run without a job (the 200 route, the probe) pays nothing.
//
//	stage "naming": SAM is done; Boxes are the boxes sent to Muse (box n =
//	                Boxes[n-1], on the 0-1000 grid of the upright photo)
//	Named:          one chunk of naming is done: box n -> the name it got
//
// The final answer (items, grouped, with their photos) still comes only at
// the end; these are hints for the screen while the donor waits.

import "context"

// Progress is one report.
type Progress struct {
	Stage string
	Boxes []Norm
	Named map[int]string
}

type progressKey struct{}

// WithProgress makes ctx carry f; the engine calls f as it goes (f must be
// safe for concurrent use: chunks are named at the same time).
func WithProgress(ctx context.Context, f func(Progress)) context.Context {
	return context.WithValue(ctx, progressKey{}, f)
}

func report(ctx context.Context, p Progress) {
	if f, ok := ctx.Value(progressKey{}).(func(Progress)); ok && f != nil {
		f(p)
	}
}

func norms(boxes []Instance) []Norm {
	out := make([]Norm, len(boxes))
	for i, b := range boxes {
		out[i] = NormBox(b)
	}
	return out
}
