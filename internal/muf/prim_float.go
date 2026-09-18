package muf

import (
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"strings"
)

// Floating-point primitives.
//
// MUF signals arithmetic trouble with the error flags rather than by failing,
// so these set a flag and return a defined value where the C would.

func init() {
	register("PI", constant(math.Pi))
	register("INF", constant(math.Inf(1)))
	register("EPSILON", constant(math.Nextafter(1, 2)-1))

	register("CEIL", float1(math.Ceil))
	register("FLOOR", float1(math.Floor))
	register("SQRT", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if x < 0 {
			// The root of a negative is imaginary, which MUF flags.
			f.ErrorFlags.Imaginary = true
			return nil, f.Push(Float(0))
		}
		return nil, f.Push(Float(math.Sqrt(x)))
	})
	register("FABS", float1(math.Abs))
	register("EXP", float1(math.Exp))
	register("SIN", float1(math.Sin))
	register("COS", float1(math.Cos))
	register("TAN", float1(math.Tan))
	register("ASIN", boundedTrig(math.Asin))
	register("ACOS", boundedTrig(math.Acos))
	register("ATAN", float1(math.Atan))
	register("ROUND", func(f *Frame) (*Result, error) {
		places, err := f.popInt()
		if err != nil {
			return nil, err
		}
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		scale := math.Pow(10, float64(places))
		return nil, f.Push(Float(math.Round(x*scale) / scale))
	})

	register("LOG", logPrim(math.Log))
	register("LOG10", logPrim(math.Log10))

	register("POW", float2(math.Pow))
	register("ATAN2", float2(math.Atan2))
	register("FMOD", float2(math.Mod))
	register("DIST3D", func(f *Frame) (*Result, error) {
		v, err := f.popFloats(3)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Float(math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
	})

	register("MODF", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		i, frac := math.Modf(x)
		if err := f.Push(Float(frac)); err != nil {
			return nil, err
		}
		return nil, f.Push(Float(i))
	})

	register("FLOAT", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		x, ok := v.asFloat()
		if !ok {
			return nil, errf("Invalid argument type.")
		}
		return nil, f.Push(Float(x))
	})
	register("STRTOF", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		x, convErr := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if convErr != nil {
			return nil, f.Push(Float(0))
		}
		return nil, f.Push(Float(x))
	})
	register("FTOSTR", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		// "%#.15g": fifteen significant digits with the trailing zeros
		// kept, which is what the '#' asks for. Go and C agree on this
		// format exactly, exponents included.
		return nil, f.Push(Str(fmt.Sprintf("%#.15g", x)))
	})
	register("FTOSTRC", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		// The compact form drops the trailing zeros.
		return nil, f.Push(Str(fmt.Sprintf("%.15g", x)))
	})

	register("FRAND", func(f *Frame) (*Result, error) {
		return nil, f.Push(Float(rand.Float64()))
	})
	register("GAUSSIAN", func(f *Frame) (*Result, error) {
		mean, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		stddev, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Float(rand.NormFloat64()*stddev + mean))
	})
}

// popFloat takes a number from the stack, accepting an integer for one.
func (f *Frame) popFloat() (float64, error) {
	v, err := f.Pop()
	if err != nil {
		return 0, err
	}
	x, ok := v.asFloat()
	if !ok {
		return 0, errf("Invalid argument type.")
	}
	return x, nil
}

// popFloats takes n numbers, leftmost first.
func (f *Frame) popFloats(n int) ([]float64, error) {
	vals, err := f.PopN(n)
	if err != nil {
		return nil, err
	}
	out := make([]float64, n)
	for i, v := range vals {
		x, ok := v.asFloat()
		if !ok {
			return nil, errf("Invalid argument type.")
		}
		out[i] = x
	}
	return out, nil
}

// constant builds a primitive that pushes a fixed value.
func constant(x float64) primFunc {
	return func(f *Frame) (*Result, error) { return nil, f.Push(Float(x)) }
}

// float1 builds a one-argument float primitive.
func float1(fn func(float64) float64) primFunc {
	return func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Float(fn(x)))
	}
}

// float2 builds a two-argument float primitive.
func float2(fn func(a, b float64) float64) primFunc {
	return func(f *Frame) (*Result, error) {
		v, err := f.popFloats(2)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Float(fn(v[0], v[1])))
	}
}

// boundedTrig builds asin and acos, whose argument must lie in [-1, 1].
func boundedTrig(fn func(float64) float64) primFunc {
	return func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if x < -1 || x > 1 {
			f.ErrorFlags.FBounds = true
			return nil, f.Push(Float(0))
		}
		return nil, f.Push(Float(fn(x)))
	}
}

// logPrim builds the logarithms, which flag rather than fail on a
// non-positive argument.
func logPrim(fn func(float64) float64) primFunc {
	return func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		switch {
		case x == 0:
			f.ErrorFlags.DivZero = true
			return nil, f.Push(Float(math.Inf(-1)))
		case x < 0:
			f.ErrorFlags.Imaginary = true
			return nil, f.Push(Float(0))
		}
		return nil, f.Push(Float(fn(x)))
	}
}
