package invoicing

import "fmt"

type Repository interface {
	Invoice(id string) (Invoice, error)
}

// MemoryRepository is what the tests run against, so that no test needs a
// substitute for the store.
type MemoryRepository struct {
	invoices map[string]Invoice
}

func NewMemoryRepository(invoices ...Invoice) *MemoryRepository {
	byID := make(map[string]Invoice, len(invoices))
	for _, inv := range invoices {
		byID[inv.ID] = inv
	}
	return &MemoryRepository{invoices: byID}
}

func (r *MemoryRepository) Invoice(id string) (Invoice, error) {
	inv, ok := r.invoices[id]
	if !ok {
		return Invoice{}, fmt.Errorf("invoice %s is not in the store", id)
	}
	return inv, nil
}
