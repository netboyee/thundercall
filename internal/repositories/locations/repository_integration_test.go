//go:build integration

package locations

import (
	"context"
	"testing"

	"thundercall-go/internal/models"
	accountsrepo "thundercall-go/internal/repositories/accounts"
	"thundercall-go/internal/testmysql"
)

func TestRepositoryMatchForMessagePolygonAndFallback(t *testing.T) {
	harness := testmysql.Open(t)
	ctx := context.Background()

	accounts := accountsrepo.New(harness.DB)
	repo := New(harness.DB)

	account := &models.Account{Name: "Integration Account", Active: true}
	if err := accounts.Create(ctx, account); err != nil {
		t.Fatalf("Create(account) error = %v", err)
	}

	location1 := &models.Location{
		AccountID:            account.ID,
		Name:                 "Polygon Match",
		CoverageWKT:          stringPtr("POINT (39.20 -84.80)"),
		CountyFIPS:           stringPtr("AAA001"),
		NWSZone:              stringPtr("AAZ001"),
		IsThunderCallEnabled: true,
		Active:               true,
	}
	location2 := &models.Location{
		AccountID:            account.ID,
		Name:                 "Fallback Match",
		CoverageWKT:          stringPtr("POINT (41.50 -86.50)"),
		CountyFIPS:           stringPtr("BBB002"),
		NWSZone:              stringPtr("BBZ002"),
		IsThunderCallEnabled: true,
		Active:               true,
	}
	location3 := &models.Location{
		AccountID:            account.ID,
		Name:                 "No Match",
		CoverageWKT:          stringPtr("POINT (42.50 -87.50)"),
		CountyFIPS:           stringPtr("CCC003"),
		NWSZone:              stringPtr("CCZ003"),
		IsThunderCallEnabled: true,
		Active:               true,
	}

	for _, location := range []*models.Location{location1, location2, location3} {
		if err := repo.Create(ctx, location); err != nil {
			t.Fatalf("Create(location %q) error = %v", location.Name, err)
		}
	}

	polygonMatches, err := repo.MatchForMessage(
		ctx,
		"POLYGON ((39.00 -85.00,39.00 -84.00,40.00 -84.00,40.00 -85.00,39.00 -85.00))",
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("MatchForMessage(polygon) error = %v", err)
	}
	if got := sortedLocationIDs(polygonMatches); len(got) != 1 || got[0] != location1.ID {
		t.Fatalf("polygon match IDs = %v, want [%d]", got, location1.ID)
	}

	fallbackMatches, err := repo.MatchForMessage(ctx, "", []string{"BBB002"}, []string{"CCZ003"})
	if err != nil {
		t.Fatalf("MatchForMessage(fallback) error = %v", err)
	}
	got := sortedLocationIDs(fallbackMatches)
	if len(got) != 2 || got[0] != location2.ID || got[1] != location3.ID {
		t.Fatalf("fallback match IDs = %v, want [%d %d]", got, location2.ID, location3.ID)
	}
}

func sortedLocationIDs(locations []models.Location) []int64 {
	ids := make([]int64, 0, len(locations))
	for _, location := range locations {
		ids = append(ids, location.ID)
	}
	if len(ids) < 2 {
		return ids
	}
	for i := 0; i < len(ids)-1; i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	return ids
}

func stringPtr(value string) *string {
	return &value
}
