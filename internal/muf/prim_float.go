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
// MUF signals arithmetic trouble with the error flags rather than by
// failing, so these set a flag and return a defined value where the C
// would.
//
// **Which** value and **which** flag is `ieee_bounds_handling`
// (`p_float.c`, seventeen sites across twelve primitives), a
// parameter that defaults **true** and had no reader here: set, the
// answer is NAN or INF and often **no flag at all**; clear, the
// answer is 0.0 and a flag is raised. Emerald answered 0.0 either
// way, and several of its flags were the wrong ones.
//
// Three of upstream's choices look like slips and are reproduced,
// because `is_set?` can see every one of them:
//
//   - `EXP` raises the NAN flag on its **successful** path
//     (`p_float.c:872`), so `1.0 exp` leaves it set.
//   - `LOG` of zero is **imaginary**, not a division by zero: the
//     `x > 0.0` test fails, so it falls into the negative branch.
//     This answered -INF with DIV_ZERO.
//   - `ASIN` and `ACOS` out of range raise **NAN**, not FBOUNDS.

func init() {
	register("PI", constant(math.Pi))
	register("INF", constant(math.Inf(1)))
	register("EPSILON", constant(math.Nextafter(1, 2)-1))

	// CEIL and FLOOR hand an infinity straight back, with FBOUNDS
	// set -- no ieee branch, because the number already is the
	// IEEE answer.
	register("CEIL", roundTo(math.Ceil))
	register("FLOOR", roundTo(math.Floor))
	register("SQRT", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if !noGood(x) {
			if x < 0 {
				// The root of a negative is
				// imaginary, which MUF flags.
				f.ErrorFlags.Imaginary = true
				return nil, f.Push(f.nanOrZero())
			}
			return nil, f.Push(Float(math.Sqrt(x)))
		}
		if math.IsNaN(x) {
			f.ErrorFlags.NaN = true
			return nil, f.Push(Float(math.NaN()))
		}
		f.ErrorFlags.FBounds = true
		return nil, f.Push(Float(x))
	})
	// FABS of an infinity is INF rather than the number, so a
	// negative infinity comes back positive -- which is the one
	// place the answer is not simply passed through.
	register("FABS", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if noGood(x) {
			f.ErrorFlags.FBounds = true
			return nil, f.Push(Float(math.Inf(1)))
		}
		return nil, f.Push(Float(math.Abs(x)))
	})
	register("EXP", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if !noGood(x) {
			// Upstream's own line, on the path that
			// worked.
			f.ErrorFlags.NaN = true
			return nil, f.Push(Float(math.Exp(x)))
		}
		return nil, f.Push(f.boundsOrZero(x, false))
	})
	register("SIN", unbounded(math.Sin))
	register("COS", unbounded(math.Cos))
	register("TAN", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if !noGood(x) {
			// Upstream tests how close the argument is to
			// an asymptote rather than trusting tan to
			// overflow, and the tolerance is DBL_EPSILON.
			r := math.Mod(x-math.Pi/2, math.Pi)
			if math.Abs(r) > dblEpsilon &&
				math.Abs(r-math.Pi) > dblEpsilon {
				return nil, f.Push(Float(math.Tan(x)))
			}
			f.ErrorFlags.FBounds = true
			return nil, f.Push(f.infOrZero())
		}
		return nil, f.Push(f.boundsOrZero(x, true))
	})
	register("ASIN", boundedTrig(math.Asin))
	register("ACOS", boundedTrig(math.Acos))
	// ATAN of an infinity is Pi/2, flagless -- the only one of
	// the family that answers a finite number and says nothing.
	register("ATAN", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if noGood(x) {
			return nil, f.Push(Float(math.Pi / 2))
		}
		return nil, f.Push(Float(math.Atan(x)))
	})
	register("ROUND", func(f *Frame) (*Result, error) {
		places, err := f.popInt()
		if err != nil {
			return nil, err
		}
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if noGood(x) {
			return nil, f.Push(f.boundsOrZero(x, true))
		}
		// modf and a half-away-from-zero nudge, which is
		// upstream's arithmetic rather than a library round.
		temp := math.Pow(10, float64(places))
		shift := temp * x
		whole := math.Trunc(shift)
		switch frac := shift - whole; {
		case frac >= 0.5:
			whole++
		case frac <= -0.5:
			whole--
		}
		return nil, f.Push(Float(whole / temp))
	})

	register("LOG", logPrim(math.Log))
	register("LOG10", logPrim(math.Log10))

	register("POW", powPrim())
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
		// The **fraction** is on top: upstream pushes
		// dresult, the integer part, and then fresult. This
		// had them the other way round.
		if noGood(x) {
			// The integer part is the argument itself and
			// the fraction is the bounded answer, which
			// is the one place the two halves disagree
			// about what went wrong.
			f.ErrorFlags.FBounds = true
			if err := f.Push(Float(x)); err != nil {
				return nil, err
			}
			return nil, f.Push(f.infOrZero())
		}
		i, frac := math.Modf(x)
		if err := f.Push(Float(i)); err != nil {
			return nil, err
		}
		return nil, f.Push(Float(frac))
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
		// `ifloat` or `number`, and **neither** is Go's
		// parser: `ifloat` (`fbstrings.c:1279`) demands a
		// decimal point with digits either side of it, so
		// "1e-300" is not a float to it and "1.0e-300" is. A
		// string that satisfies neither answers 0.0 and
		// raises the **NAN** flag, which this raised for
		// nothing.
		if !isIFloat(s) && !isNumberStr(s) {
			f.ErrorFlags.NaN = true
			return nil, f.Push(Float(0))
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
		// "%#.15g": fifteen significant digits with the
		// trailing zeros kept, which is what the '#' asks
		// for. Go and C agree on this format exactly,
		// exponents included — except for the three values
		// that are not numbers.
		if s, ok := cSpecial(x); ok {
			return nil, f.Push(Str(s))
		}
		return nil, f.Push(Str(fmt.Sprintf("%#.15g", x)))
	})
	register("FTOSTRC", func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		// The compact form drops the trailing zeros and then
		// **puts one back**: upstream appends ".0" unless the
		// result already holds a '.', an 'e' or an 'n'
		// (`p_float.c:1164`), so "0" becomes "0.0" while
		// "1e+20", "nan" and "inf" are left alone — the 'n'
		// catching "nan" and "inf" alike. Without it every
		// whole number printed as an integer.
		out, ok := cSpecial(x)
		if !ok {
			out = fmt.Sprintf("%.15g", x)
		}
		if !strings.ContainsAny(out, ".en") {
			out += ".0"
		}
		return nil, f.Push(Str(out))
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

// popFloat takes a number from the stack, accepting an integer for
// one.
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

// isIFloat is `ifloat` (`fbstrings.c:1279`): leading whitespace, an
// optional sign, digits, a '.', digits, and optionally an exponent
// — and then the **end of the string**, so a trailing space fails.
//
// The decimal point is not optional, which is the surprising half.
// Upstream's own commented-out block beside it wishes "inf" and "nan"
// were accepted; they are not.
func isIFloat(s string) bool {
	i := 0
	for i < len(s) && isSpaceByte(s[i]) {
		i++
	}
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i >= len(s) || i == start {
		return false
	}
	if s[i] != '.' {
		return false
	}
	i++
	start = i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == start {
		return false
	}
	if i == len(s) {
		return true
	}
	if s[i] != 'e' && s[i] != 'E' {
		return false
	}
	i++
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	start = i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i != start && i == len(s)
}

// isNumberStr is `number` (`fbstrings.c:1256`): leading whitespace,
// an optional sign, and then digits to the end.
func isNumberStr(s string) bool {
	i := 0
	for i < len(s) && isSpaceByte(s[i]) {
		i++
	}
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	if i == len(s) {
		return false
	}
	for ; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// pushZeros is the answer both coordinate conversions give for an
// input they cannot use.
func (f *Frame) pushZeros(n int) error {
	for range n {
		if err := f.Push(Float(0)); err != nil {
			return err
		}
	}
	return nil
}

// cSpecial spells the three values C's printf spells differently from
// Go's: "nan", "inf" and "-inf" against "NaN", "+Inf" and "-Inf".
// Every float a program prints goes through FTOSTR or FTOSTRC, so
// this is the difference between a world's own arithmetic reports
// matching the C and not.
func cSpecial(x float64) (string, bool) {
	switch {
	case math.IsNaN(x):
		// A NaN carries a sign bit and C's printf prints it,
		// so a NaN made by dividing zero by zero really does
		// come out "-nan" on this hardware.
		if math.Signbit(x) {
			return "-nan", true
		}
		return "nan", true
	case math.IsInf(x, 1):
		return "inf", true
	case math.IsInf(x, -1):
		return "-inf", true
	}
	return "", false
}

// dblEpsilon is C's DBL_EPSILON, the tolerance TAN compares against.
const dblEpsilon = 2.220446049250313e-16

// noGood is `no_good` (`fbmath.c:104`): an infinity or a NaN.
func noGood(x float64) bool {
	return math.IsInf(x, 0) || math.IsNaN(x)
}

// ieeeBounds reads `ieee_bounds_handling`.
//
// A frame with no host — a unit test's — gets the **default**,
// which is true, rather than the clear branch.
func (f *Frame) ieeeBounds() bool {
	if f.host == nil {
		return true
	}
	return f.host.TuneBool("ieee_bounds_handling")
}

// nanOrZero, infOrZero and boundsOrZero are the three shapes the
// parameter takes. The first two raise no flag of their own, because
// their callers have already raised the right one; boundsOrZero
// raises one **only** on the clear branch, which is upstream's and is
// the half that is easiest to get backwards.
func (f *Frame) nanOrZero() Value {
	if f.ieeeBounds() {
		return Float(math.NaN())
	}
	return Float(0)
}

func (f *Frame) infOrZero() Value {
	if f.ieeeBounds() {
		return Float(math.Inf(1))
	}
	return Float(0)
}

// boundsOrZero is the tail EXP, TAN, ROUND and MODF share for an
// input that is already infinite or NaN: the IEEE answer is the kind
// of badness the input was, and the clear branch is zero and a flag.
//
// nanFlag says which flag that is for a **NaN** input. TAN and ROUND
// raise NAN there and EXP raises FBOUNDS, which is upstream's and not
// a distinction either function's shape would suggest.
func (f *Frame) boundsOrZero(x float64, nanFlag bool) Value {
	if f.ieeeBounds() {
		if math.IsNaN(x) {
			return Float(math.NaN())
		}
		return Float(math.Inf(1))
	}
	if math.IsNaN(x) && nanFlag {
		f.ErrorFlags.NaN = true
	} else {
		f.ErrorFlags.FBounds = true
	}
	return Float(0)
}

// nanOrBounds is SIN's and COS's tail, which does **not** tell a NaN
// from an infinity: either one answers NAN, so `inf sin` is a NaN
// rather than an infinity.
func (f *Frame) nanOrBounds() Value {
	if f.ieeeBounds() {
		return Float(math.NaN())
	}
	f.ErrorFlags.FBounds = true
	return Float(0)
}

// roundTo builds CEIL and FLOOR, which hand an infinity back
// unchanged.
func roundTo(fn func(float64) float64) primFunc {
	return func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if noGood(x) {
			f.ErrorFlags.FBounds = true
			return nil, f.Push(Float(x))
		}
		return nil, f.Push(Float(fn(x)))
	}
}

// unbounded builds SIN and COS, which are defined everywhere a float
// can be and so have only the bad-input tail — where a NaN and an
// infinity are **not** told apart.
func unbounded(fn func(float64) float64) primFunc {
	return func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if !noGood(x) {
			return nil, f.Push(Float(fn(x)))
		}
		return nil, f.Push(f.nanOrBounds())
	}
}

// boundedTrig builds asin and acos, whose argument must lie in [-1,
// 1]. A NaN fails that test too and takes the same branch.
func boundedTrig(fn func(float64) float64) primFunc {
	return func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		if x >= -1 && x <= 1 {
			return nil, f.Push(Float(fn(x)))
		}
		if f.ieeeBounds() {
			return nil, f.Push(Float(math.NaN()))
		}
		f.ErrorFlags.NaN = true
		return nil, f.Push(Float(0))
	}
}

// logPrim builds the logarithms, which flag rather than fail on a
// non-positive argument.
//
// There is no division-by-zero branch: zero fails the `x > 0.0` test,
// so it is **imaginary** along with every negative, and a NaN is too.
func logPrim(fn func(float64) float64) primFunc {
	return func(f *Frame) (*Result, error) {
		x, err := f.popFloat()
		if err != nil {
			return nil, err
		}
		switch {
		case !noGood(x) && x > 0:
			return nil, f.Push(Float(fn(x)))
		case x > 0:
			// A positive infinity, which has its own
			// answer and is not subject to the parameter.
			f.ErrorFlags.FBounds = true
			return nil, f.Push(Float(math.Inf(1)))
		}
		f.ErrorFlags.Imaginary = true
		return nil, f.Push(f.nanOrZero())
	}
}

// powPrim builds POW and "**". The base is the lower operand and the
// exponent the upper, and a near-zero base short-circuits to zero
// before either is looked at further.
func powPrim() primFunc {
	return func(f *Frame) (*Result, error) {
		v, err := f.popFloats(2)
		if err != nil {
			return nil, err
		}
		base, exp := v[0], v[1]
		if noGood(base) || noGood(exp) {
			f.ErrorFlags.FBounds = true
			return nil, f.Push(f.infOrZero())
		}
		switch {
		case math.Abs(base) < dblEpsilon:
			return nil, f.Push(Float(0))
		case base < 0 && exp != math.Floor(exp):
			f.ErrorFlags.Imaginary = true
			return nil, f.Push(f.nanOrZero())
		}
		return nil, f.Push(Float(math.Pow(base, exp)))
	}
}

// The remaining float primitives: the power operator and the
// coordinate conversions.

func init() {
	register("**", powPrim())

	register("DIFF3", func(f *Frame) (*Result, error) {
		// Two points, six numbers, giving the vector between
		// them.
		v, err := f.popFloats(6)
		if err != nil {
			return nil, err
		}
		// The vector runs from the first point to the second.
		for i := 0; i < 3; i++ {
			if err := f.Push(Float(v[i+3] - v[i])); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	register("XYZ_TO_POLAR", func(f *Frame) (*Result, error) {
		v, err := f.popFloats(3)
		if err != nil {
			return nil, err
		}
		x, y, z := v[0], v[1], v[2]
		if noGood(x) || noGood(y) || noGood(z) {
			// Three zeros and the NAN flag, which is
			// upstream's answer for either conversion.
			f.ErrorFlags.NaN = true
			return nil, f.pushZeros(3)
		}
		r := math.Sqrt(x*x + y*y + z*z)
		if r == 0 {
			for range 3 {
				if err := f.Push(Float(0)); err != nil {
					return nil, err
				}
			}
			return nil, nil
		}
		theta := math.Atan2(y, x)
		phi := math.Acos(z / r)
		for _, out := range []float64{r, theta, phi} {
			if err := f.Push(Float(out)); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})

	register("POLAR_TO_XYZ", func(f *Frame) (*Result, error) {
		v, err := f.popFloats(3)
		if err != nil {
			return nil, err
		}
		r, theta, phi := v[0], v[1], v[2]
		if noGood(r) || noGood(theta) || noGood(phi) {
			f.ErrorFlags.NaN = true
			return nil, f.pushZeros(3)
		}
		out := []float64{
			r * math.Sin(phi) * math.Cos(theta),
			r * math.Sin(phi) * math.Sin(theta),
			r * math.Cos(phi),
		}
		for _, x := range out {
			if err := f.Push(Float(x)); err != nil {
				return nil, err
			}
		}
		return nil, nil
	})
}
