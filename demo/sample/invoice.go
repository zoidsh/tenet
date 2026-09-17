package invoicing

// Line is one billable item. UnitCents is a whole number of cents, so no
// price picks up a rounding error on its way through an invoice.
type Line struct {
	Description string
	Quantity    int
	UnitCents   int
}

type Invoice struct {
	ID       string
	Customer string
	Lines    []Line
}

func (inv Invoice) SubtotalCents() int {
	subtotal := 0
	for _, line := range inv.Lines {
		subtotal += line.Quantity * line.UnitCents
	}
	return subtotal
}

// TaxCents rounds half up, which is how the revenue office's own worked
// examples round, and takes the rate per mille so it stays an integer.
func TaxCents(subtotalCents, ratePerMille int) int {
	return (subtotalCents*ratePerMille + 500) / 1000
}
