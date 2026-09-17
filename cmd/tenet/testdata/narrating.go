package testdata

func total(values []int) int {
	sum := 0
	// loop over the values and add each one to sum
	for _, v := range values {
		sum += v
	}
	return sum
}
