package thundercall

import "testing"

func TestGenerateFingerprintIgnoresOrdering(t *testing.T) {
	t.Parallel()

	first := GenerateFingerprint(
		"Tornado Warning",
		"POLYGON ((-80.0 35.0, -81.0 35.0, -81.0 36.0, -80.0 35.0))",
		[]string{"037", "001"},
		[]string{"NCZ001", "NCZ002"},
	)

	second := GenerateFingerprint(
		" tornado warning ",
		"polygon ((-81.0 35.0, -80.0 35.0, -80.0 35.0, -81.0 36.0))",
		[]string{"001", "037"},
		[]string{"NCZ002", "NCZ001"},
	)

	if first != second {
		t.Fatalf("expected fingerprints to match: %s != %s", first, second)
	}
}
