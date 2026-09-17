package invoicing

import "testing"

func TestTotalCentsAddsTax(t *testing.T) {
	repo := NewMemoryRepository(Invoice{
		ID:       "INV-1041",
		Customer: "Blue Harbour Cafe",
		Lines:    []Line{{Description: "Design review", Quantity: 2, UnitCents: 25000}},
	})

	total, err := TotalCents(repo, "INV-1041", 200)
	if err != nil {
		t.Fatalf("TotalCents: %v", err)
	}
	if total != 60000 {
		t.Errorf("total = %d cents, want 60000", total)
	}
}

func TestTotalCentsReportsAMissingInvoice(t *testing.T) {
	if _, err := TotalCents(NewMemoryRepository(), "INV-9", 200); err == nil {
		t.Fatal("TotalCents on an empty store: want an error, got nil")
	}
}
