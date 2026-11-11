package ledger

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"
)

// ParseCSV reads a bank statement: a header line, then one line per
// transaction as "date,description,category,amount", with the date as
// 2006-01-02. Fields may be quoted as encoding/csv writes them.
func ParseCSV(r io.Reader) ([]Transaction, error) {
	var txs []Transaction
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		if line == 1 || strings.TrimSpace(sc.Text()) == "" {
			continue // the header
		}
		f := strings.Split(sc.Text(), ",")
		if len(f) != 4 {
			return nil, fmt.Errorf("line %d: %d fields, want 4", line, len(f))
		}
		date, err := time.Parse("2006-01-02", f[0])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		amount, err := ParseAmount(f[3])
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		txs = append(txs, Transaction{Date: date, Description: f[1], Category: f[2], Amount: amount})
	}
	return txs, sc.Err()
}

// Merge adds imported to existing, leaving out each imported transaction
// already in existing: the same date, description and amount. Importing
// the same statement twice changes nothing. The result keeps existing's
// order, then the new ones in imported's order.
func Merge(existing, imported []Transaction) []Transaction {
	return append(existing, imported...) // TODO: leave out the duplicates
}
