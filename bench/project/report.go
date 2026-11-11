package ledger

// CategoryTotal is the money spent in one category.
type CategoryTotal struct {
	Category string
	Spent    Money // positive
}

// TopSpending returns the n categories where the most was spent, the
// largest first, ties by category name. Only spending (negative amounts)
// counts, reported as positive totals; categories with no spending are
// left out.
func TopSpending(txs []Transaction, n int) []CategoryTotal {
	return nil // TODO
}

// Search returns the transactions in which every word of query appears,
// in the description or in the category, ignoring case; in their order.
// An empty query matches nothing.
func Search(txs []Transaction, query string) []Transaction {
	return nil // TODO
}
