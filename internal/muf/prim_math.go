package muf

import (
	"math"
	"strconv"
	"strings"

	"github.com/FatmanUK/fuzzball_emerald/internal/ascii"
	"github.com/FatmanUK/fuzzball_emerald/internal/ref"
)

// Arithmetic, comparison and conversion primitives.

func init() {
	register("+", arith('+'))
	register("-", arith('-'))
	register("*", arith('*'))
	register("/", arith('/'))
	register("%", arith('%'))

	register("<", compare(func(c int) bool { return c < 0 }))
	register(">", compare(func(c int) bool { return c > 0 }))
	register("<=", compare(func(c int) bool { return c <= 0 }))
	register(">=", compare(func(c int) bool { return c >= 0 }))

	register("=", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(v[0].Equal(v[1])))
	})

	register("NOT", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(!v.Truthy()))
	})
	register("AND", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(v[0].Truthy() && v[1].Truthy()))
	})
	register("OR", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(v[0].Truthy() || v[1].Truthy()))
	})
	register("XOR", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(v[0].Truthy() != v[1].Truthy()))
	})

	register("ABS", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		if n < 0 {
			n = -n
		}
		return nil, f.Push(Int(n))
	})
	register("SIGN", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		switch {
		case n > 0:
			return nil, f.Push(Int(1))
		case n < 0:
			return nil, f.Push(Int(-1))
		}
		return nil, f.Push(Int(0))
	})
	register("INT", func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		switch v.Type {
		case TypeInteger:
			return nil, f.Push(v)
		case TypeFloat:
			if math.IsNaN(v.Float) ||
				math.IsInf(v.Float, 0) {
				return nil, errf("Invalid argument type.")
			}
			return nil, f.Push(Int(int64(v.Float)))
		case TypeObject:
			return nil, f.Push(Int(int64(v.Ref)))
		}
		return nil, errf("Invalid argument type.")
	})
	register("DBREF", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Obj(ref.Ref(n)))
	})
	register("ATOI", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		// MUF's atoi takes the leading integer and yields
		// zero when there is none, as C's does.
		return nil, f.Push(Int(leadingInt(s)))
	})
	register("NUMBER?", func(f *Frame) (*Result, error) {
		s, err := f.popStr()
		if err != nil {
			return nil, err
		}
		_, convErr := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return nil, f.Push(Bool(convErr == nil))
	})

	register("IS_SET?", func(f *Frame) (*Result, error) {
		n, err := f.popErrorFlag()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(n >= 0 &&
			f.ErrorFlags.Get(n)))
	})
	register("CLEAR", func(f *Frame) (*Result, error) {
		f.ErrorFlags.Clear()
		return nil, nil
	})
	register("ERROR?", func(f *Frame) (*Result, error) {
		e := f.ErrorFlags
		return nil, f.Push(Bool(e.DivZero || e.NaN || e.Imaginary ||
			e.FBounds || e.IBounds))
	})
	// SET_ERROR and CLEAR_ERROR **push a result** -- 1 when the
	// flag was resolved and 0 when it was not -- which Emerald's
	// pushed nothing at all, so every program using one was a
	// stack item short from there on.
	register("SET_ERROR", errorWrite(true))
	register("CLEAR_ERROR", errorWrite(false))

	register("BITOR", bitwise(func(a, b int64) int64 { return a | b }))
	register("BITAND", bitwise(func(a, b int64) int64 { return a & b }))
	register("BITXOR", bitwise(func(a, b int64) int64 { return a ^ b }))
	register("BITSHIFT", bitwise(func(a, b int64) int64 {
		if b >= 0 {
			if b >= 64 {
				return 0
			}
			return a << uint(b)
		}
		if -b >= 64 {
			return 0
		}
		return a >> uint(-b)
	}))
}

// errorWrite builds SET_ERROR and CLEAR_ERROR.
func errorWrite(to bool) primFunc {
	return func(f *Frame) (*Result, error) {
		n, err := f.popErrorFlag()
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, f.Push(Int(0))
		}
		f.ErrorFlags.Set(n, to)
		return nil, f.Push(Int(1))
	}
}

// arith builds an arithmetic primitive, promoting to float when
// either side is one, as MUF does.
func arith(op byte) primFunc {
	return func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		a, b := v[0], v[1]

		if a.Type == TypeFloat || b.Type == TypeFloat {
			// `prim_mod` refuses a float outright
			// (`p_math.c:481`), where this answered
			// fmod's result.
			if op == '%' {
				return nil, errf(badArgType)
			}
			x, xok := a.asFloat()
			y, yok := b.asFloat()
			if !xok || !yok {
				return nil, errf("Invalid argument type.")
			}
			return nil, f.Push(f.floatArith(op, x, y))
		}

		if a.Type != TypeInteger || b.Type != TypeInteger {
			return nil, errf("Invalid argument type.")
		}
		switch op {
		case '+':
			return nil, f.Push(Int(a.Num + b.Num))
		case '-':
			return nil, f.Push(Int(a.Num - b.Num))
		case '*':
			return nil, f.Push(Int(a.Num * b.Num))
		case '/', '%':
			// Dividing by zero is not a failure in MUF:
			// the result is zero and a flag is raised,
			// which a program reads with is_set?.
			// Aborting here would end programs that
			// upstream runs to completion.
			//
			// Only `/` raises it, though: `prim_mod`
			// (`p_math.c:485`) answers zero and says
			// nothing at all.
			if b.Num == 0 {
				if op == '/' {
					f.ErrorFlags.DivZero = true
				}
				return nil, f.Push(Int(0))
			}
			// The one case where the quotient does not
			// fit, which would panic in Go and raises a
			// bounds flag upstream.
			if a.Num == math.MinInt64 && b.Num == -1 {
				f.ErrorFlags.IBounds = true
				return nil, f.Push(Int(0))
			}
			if op == '/' {
				return nil, f.Push(Int(a.Num / b.Num))
			}
			return nil, f.Push(Int(a.Num % b.Num))
		}
		return nil, errf("unknown operator")
	}
}

// badArgType is what every one of these says for an operand it cannot
// use.
const badArgType = "Invalid argument type."

// floatArith is the float half of `+`, `-`, `*` and `/`
// (`p_math.c:125`, `:197`, `:314`, `:385`), which is more than the
// arithmetic: `ieee_bounds_handling` decides what an infinite or NaN
// operand produces, and division by zero is a flag rather than a
// failure.
//
// Three details are upstream's. The ieee branch for an **infinite**
// operand *recomputes* the operation rather than answering a fixed
// INF, so `inf 1.0 -` is an infinity and `inf inf -` is a NaN. A NaN
// operand is told apart from an infinite one and raises the NAN flag
// where an infinity raises FBOUNDS. And a zero divisor is tested with
// DBL_EPSILON rather than against zero, so a very small float divisor
// is a division by zero too.
func (f *Frame) floatArith(op byte, x, y float64) Value {
	if op == '/' {
		if math.Abs(y) < dblEpsilon {
			// The flag is raised either way; what differs
			// is whether the sign and kind of the
			// numerator survive into the answer.
			f.ErrorFlags.DivZero = true
			if f.ieeeBounds() {
				return Float(x * math.Inf(1))
			}
			return Float(math.Inf(1))
		}
	}
	if !noGood(x) && !noGood(y) {
		return Float(floatOp(op, x, y))
	}
	if math.IsNaN(x) || math.IsNaN(y) {
		if f.ieeeBounds() {
			return Float(math.NaN())
		}
		f.ErrorFlags.NaN = true
		return Float(0)
	}
	if f.ieeeBounds() {
		return Float(floatOp(op, x, y))
	}
	f.ErrorFlags.FBounds = true
	return Float(0)
}

func floatOp(op byte, x, y float64) float64 {
	switch op {
	case '+':
		return x + y
	case '-':
		return x - y
	case '*':
		return x * y
	}
	return x / y
}

// compare builds a comparison primitive. Numbers compare numerically
// and strings compare case-insensitively, as MUF's operators do.
func compare(ok func(int) bool) primFunc {
	return func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		a, b := v[0], v[1]

		if a.Type == TypeString && b.Type == TypeString {
			return nil, f.Push(Bool(ok(ascii.Compare(a.Str, b.Str))))
		}
		x, xok := a.asFloat()
		y, yok := b.asFloat()
		if !xok || !yok {
			return nil, errf("Invalid argument type.")
		}
		switch {
		case x < y:
			return nil, f.Push(Bool(ok(-1)))
		case x > y:
			return nil, f.Push(Bool(ok(1)))
		}
		return nil, f.Push(Bool(ok(0)))
	}
}

// bitwise builds an integer bit-manipulation primitive.
func bitwise(op func(a, b int64) int64) primFunc {
	return func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		if v[0].Type != TypeInteger ||
			v[1].Type != TypeInteger {
			return nil, errf("Invalid argument type.")
		}
		return nil, f.Push(Int(op(v[0].Num, v[1].Num)))
	}
}

// leadingInt reads the integer a string starts with, yielding zero
// when it does not start with one.
func leadingInt(s string) int64 {
	s = strings.TrimLeft(s, " \t")
	end := 0
	if end < len(s) && (s[end] == '-' || s[end] == '+') {
		end++
	}
	digits := end
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == digits {
		return 0
	}
	n, err := strconv.ParseInt(s[:end], 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// Operator aliases and the increment family.

func init() {
	register("&", bitwise(func(a, b int64) int64 { return a & b }))
	register("|", bitwise(func(a, b int64) int64 { return a | b }))
	register("^", bitwise(func(a, b int64) int64 { return a ^ b }))
	register("<<", bitwise(func(a, b int64) int64 {
		if b < 0 || b >= 64 {
			return 0
		}
		return a << uint(b)
	}))

	register("!=", func(f *Frame) (*Result, error) {
		v, err := f.PopN(2)
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(!v[0].Equal(v[1])))
	})

	register("++", step1(1))
	register("--", step1(-1))
}

// step1 builds ++ and --, which add one to a number or to a dbref.
func step1(by int64) primFunc {
	return func(f *Frame) (*Result, error) {
		v, err := f.Pop()
		if err != nil {
			return nil, err
		}
		switch v.Type {
		case TypeInteger:
			return nil, f.Push(Int(v.Num + by))
		case TypeFloat:
			return nil, f.Push(Float(v.Float + float64(by)))
		case TypeObject:
			return nil, f.Push(Obj(v.Ref + ref.Ref(by)))
		}
		return nil, errf("Invalid datatype.")
	}
}
