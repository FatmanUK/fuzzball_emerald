package ascii

// AlphanumCompare orders strings the way a person expects a list of names
// with numbers in them to sort: "item2" before "item10", not after.
//
// This is upstream's alphanum_compare. It walks both strings while they agree
// case-insensitively, and if both then sit on a digit it compares the numbers
// those digits spell rather than the characters. Leading zeros are backed
// over first, so "007" and "7" compare as the same number.
//
// The result is a difference rather than a -1/0/1, as the C's is, because
// several of its callers compare it against zero in both directions.
func AlphanumCompare(a, b string) int {
	i, j := 0, 0
	for i < len(a) && j < len(b) && lower(a[i]) == lower(b[j]) {
		i++
		j++
	}

	if !(isDigit(byteAt(a, i)) && isDigit(byteAt(b, j))) {
		return int(lower(byteAt(a, i))) - int(lower(byteAt(b, j)))
	}

	// Both sit on a digit, so compare the numbers. u1 and u2 remember where
	// they diverged, for the fallbacks that compare characters after all.
	u1, u2 := i, j

	// Back up over zeros, so a padded number compares by value. The first
	// step back is taken only when the character under b is itself a zero —
	// the two differ there, and it is b's zero that made them differ.
	if i > 0 && byteAt(b, j) == '0' {
		i--
		j--
	}
	for i > 0 && byteAt(a, i) == '0' {
		i--
		j--
	}
	if !isDigit(byteAt(a, i)) {
		i++
		j++
	}

	n1, cnt1 := 0, 0
	for isDigit(byteAt(a, i)) {
		cnt1++
		n1 = n1*10 + int(a[i]-'0')
		i++
	}
	n2, cnt2 := 0, 0
	for isDigit(byteAt(b, j)) {
		cnt2++
		n2 = n2*10 + int(b[j]-'0')
		j++
	}

	// A number too long to have been accumulated accurately is ordered by
	// how many digits it has instead.
	if cnt1 > 8 || cnt2 > 8 {
		if cnt1 == cnt2 {
			return int(byteAt(a, u1)) - int(byteAt(b, u2))
		}
		return cnt1 - cnt2
	}
	if n1 != 0 && n2 != 0 && n1 != n2 {
		return n1 - n2
	}
	return int(byteAt(a, u1)) - int(byteAt(b, u2))
}

// byteAt reads one byte, treating the end of the string as the NUL the C
// reads there.
func byteAt(s string, i int) byte {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
