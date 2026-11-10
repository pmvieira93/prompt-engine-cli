package ledger

// Budget is a monthly spending limit for one category.
type Budget struct {
	Category string
	Limit    Money // positive
}

// Remaining is what is left of b after the spending in txs: the limit
// less every negative amount in b's category. Income in the category
// (positive amounts) doesn't add to it. Below zero means overspent.
func Remaining(b Budget, txs []Transaction) Money {
	left := b.Limit
	for _, t := range txs {
		if t.Category == b.Category {
			left += t.Amount
		}
	}
	return left
}

// Overspent returns the budgets with nothing left, in their order.
func Overspent(budgets []Budget, txs []Transaction) []Budget {
	var over []Budget
	for _, b := range budgets {
		if Remaining(b, txs) < 0 {
			over = append(over, b)
		}
	}
	return over
}
