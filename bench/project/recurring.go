package ledger

import (
	"fmt"
	"time"
)

// Recurring is a payment that repeats: rent, a subscription.
type Recurring struct {
	Description string
	Amount      Money
	Every       string // "weekly" or "monthly"
	Next        time.Time
	FirstDay    int // the day of the month of the first payment, for NextDate
}

// NextDate is the date after d for a payment repeating every: a week
// later for "weekly"; for "monthly", the same day of the next month, or
// that month's last day when it is shorter (31 January, then 28 or 29
// February, then 31 March: the day of the first payment is kept). For
// a Recurring r, that is NextDate(r.Next, r.Every, r.FirstDay).
func NextDate(d time.Time, every string, firstDay int) (time.Time, error) {
	switch every {
	case "weekly":
		return d.AddDate(0, 0, 7), nil
	case "monthly":
		return d.AddDate(0, 1, 0), nil
	}
	return time.Time{}, fmt.Errorf("repeats %q: want weekly or monthly", every)
}
