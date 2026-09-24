package store

import (
	"context"
	"math"
	"testing"

	"github.com/FatmanUK/fuzzball_emerald/internal/props"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
	"github.com/FatmanUK/fuzzball_emerald/internal/world"
)

// TestFloatPropertiesRoundTrip checks the awkward end of the float
// range. MUF programs can and do store infinities in properties, and
// a decimal column would not carry them.
func TestFloatPropertiesRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	vals := map[string]float64{
		"exact":  1.5,
		"tenth":  0.1,
		"third":  -1.0 / 3.0,
		"pi":     math.Pi,
		"e":      math.E,
		"tiny":   math.SmallestNonzeroFloat64,
		"huge":   math.MaxFloat64,
		"inf":    math.Inf(1),
		"neginf": math.Inf(-1),
		"nan":    math.NaN(),
	}

	w := world.New()
	o := w.Create("Thing", ref.TypeThing, ref.God)
	for k, v := range vals {
		w.SetProp(o.Ref, k, props.Value{Type: props.Float, Float: v})
	}
	if err := s.Flush(ctx, w.TakeSnapshot()); err != nil {
		t.Fatal(err)
	}

	reloaded := world.New()
	if _, err := s.Load(ctx, reloaded); err != nil {
		t.Fatal(err)
	}
	for k, want := range vals {
		got, ok := reloaded.Get(o.Ref).Props.Get(k)
		if !ok {
			t.Errorf("%s: missing after reload", k)
			continue
		}
		// NaN is never equal to itself, so it needs its own
		// check.
		if math.IsNaN(want) {
			if !math.IsNaN(got.Float) {
				t.Errorf("%s = %v, want NaN", k, got.Float)
			}
			continue
		}
		if got.Float != want {
			t.Errorf("%s = %v (bits %#x), want %v (bits %#x)", k,
				got.Float, math.Float64bits(got.Float),
				want, math.Float64bits(want))
		}
	}
}
