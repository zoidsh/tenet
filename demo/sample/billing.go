package invoicing

func TotalCents(repo Repository, id string, taxRatePerMille int) (int, error) {
	inv, err := repo.Invoice(id)
	if err != nil {
		return 0, err
	}
	subtotal := inv.SubtotalCents()
	return subtotal + TaxCents(subtotal, taxRatePerMille), nil
}
