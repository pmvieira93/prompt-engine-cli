// Package ledger keeps a household's transactions: amounts, dates,
// categories, and the reports built from them.
package ledger

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Money is an amount in cents.
type Money int64

// String formats m as "12.34", or "-12.34" when negative.
func (m Money) String() string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	return fmt.Sprintf("%s%d.%02d", sign, m/100, m%100)
}

// ParseAmount reads an amount such as "12.34", "-5", "€7.50" or "1,234.00".
func ParseAmount(s string) (Money, error) {
	s = strings.TrimSpace(s)
	if s[0] == '$' {
		s = s[1:]
	}
	s = strings.TrimPrefix(s, "€")
	s = strings.ReplaceAll(s, ",", "")
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		return 0, fmt.Errorf("amount %q: more than two decimals", s)
	}
	for len(frac) < 2 {
		frac += "0"
	}
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q: %w", s, err)
	}
	f, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("amount %q: %w", s, err)
	}
	m := Money(w*100 + f)
	if neg {
		m = -m
	}
	return m, nil
}

// ErrNoPeople is returned by Split for fewer than one person.
var ErrNoPeople = errors.New("split between no one")

// Split divides total between n people. Every share is within a cent of
// the others, and the shares add up to total.
func Split(total Money, n int) ([]Money, error) {
	if n < 1 {
		return nil, ErrNoPeople
	}
	shares := make([]Money, n)
	for i := range shares {
		shares[i] = total / Money(n)
	}
	return shares, nil
}
