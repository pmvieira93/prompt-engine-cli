package ledger

import "io"

// ExportCSV writes txs as CSV: a header line "date,description,category,amount",
// then one line per transaction, with the date as 2006-01-02 and the
// amount as Money.String formats it. Fields with commas or quotes are
// quoted as encoding/csv does.
func ExportCSV(w io.Writer, txs []Transaction) error {
	return ErrNotImplemented // TODO
}
