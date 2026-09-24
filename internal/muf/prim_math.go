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
	register("INTOSTR", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Str(strconv.FormatInt(n, 10)))
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
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		return nil, f.Push(Bool(f.ErrorFlags.Get(int(n))))
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
	register("SET_ERROR", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		f.ErrorFlags.Set(int(n), true)
		return nil, nil
	})
	register("CLEAR_ERROR", func(f *Frame) (*Result, error) {
		n, err := f.popInt()
		if err != nil {
			return nil, err
		}
		f.ErrorFlags.Set(int(n), false)
		return nil, nil
	})

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
			x, xok := a.asFloat()
			y, yok := b.asFloat()
			if !xok || !yok {
				return nil, errf("Invalid argument type.")
			}
			switch op {
			case '+':
				return nil, f.Push(Float(x + y))
			case '-':
				return nil, f.Push(Float(x - y))
			case '*':
				return nil, f.Push(Float(x * y))
			case '/':
				// Float division by zero yields an
				// infinity rather than failing, which
				// is what the float error mask is
				// for.
				return nil, f.Push(Float(x / y))
			case '%':
				return nil, f.Push(Float(math.Mod(x, y)))
			}
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
			if b.Num == 0 {
				f.ErrorFlags.DivZero = true
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
