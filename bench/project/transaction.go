package ledger

import (
	"errors"
	"sort"
	"time"
)

// Transaction is one entry in the ledger.
type Transaction struct {
	Date        time.Time
	Description string
	Category    string
	Amount      Money // negative for spending
}

// SortByDate sorts txs from the oldest to the newest. Transactions on the
// same day keep their order.
func SortByDate(txs []Transaction) {
	sort.SliceStable(txs, func(i, j int) bool {
		return txs[i].Date.After(txs[j].Date)
	})
}

// Balance is the sum of every amount in txs.
func Balance(txs []Transaction) Money {
	var total Money
	for _, t := range txs {
		total += t.Amount
	}
	return total
}

// ErrNotImplemented is returned by features still to be written.
var ErrNotImplemented = errors.New("not implemented")

// FilterByCategory returns the transactions in category, in their order.
// Categories match regardless of case and of spaces around them.
func FilterByCategory(txs []Transaction, category string) []Transaction {
	return nil // TODO
}

// MonthlyTotals sums the amounts per calendar month, keyed "2006-01".
func MonthlyTotals(txs []Transaction) map[string]Money {
	return nil // TODO
}
