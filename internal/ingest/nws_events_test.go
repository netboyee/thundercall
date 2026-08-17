package ingest

import (
	"context"
	"testing"
	"time"

	"thundercall-go/internal/models"
	"thundercall-go/internal/thundercall"
)

func TestResolveNWSEventUsesPrimaryVTECBeginYear(t *testing.T) {
	t.Parallel()

	repo := &fakeNWSEventsRepository{}
	req := thundercall.IncomingMessageRequest{
		PrimaryVTECCount: 1,
		VTECProductClass: "O",
		VTECAction:       "NEW",
		VTECOfficeID:     "KAKQ",
		VTECPhenomenon:   "SV",
		VTECSignificance: "W",
		VTECETN:          "0273",
		VTECBeginsAtRaw:  "260817T0259Z",
		VTECBeginsAt:     time.Date(2026, 8, 17, 2, 59, 0, 0, time.UTC),
		VTECEndsAtRaw:    "260817T0345Z",
		VTECEndsAt:       time.Date(2026, 8, 17, 3, 45, 0, 0, time.UTC),
		Timestamp:        time.Date(2026, 8, 17, 2, 59, 0, 0, time.UTC),
	}

	event, err := resolveNWSEvent(context.Background(), repo, req)
	if err != nil {
		t.Fatalf("resolveNWSEvent() error = %v", err)
	}
	if event == nil {
		t.Fatal("resolveNWSEvent() = nil, want event")
	}
	if event.EventKey != "O:KAKQ:SV:W:0273:2026" {
		t.Fatalf("EventKey = %q, want O:KAKQ:SV:W:0273:2026", event.EventKey)
	}
	if repo.createCalls != 1 {
		t.Fatalf("createCalls = %d, want 1", repo.createCalls)
	}
	if repo.updateCalls != 0 {
		t.Fatalf("updateCalls = %d, want 0", repo.updateCalls)
	}
}

func TestResolveNWSEventFallsBackToPriorYearForZeroBeginTime(t *testing.T) {
	t.Parallel()

	existing := &models.NWSEvent{
		ID:           99,
		EventKey:     "O:KAKQ:SV:W:0273:2026",
		ProductClass: "O",
		OfficeID:     "KAKQ",
		Phenomenon:   "SV",
		Significance: "W",
		ETN:          "0273",
		EventYear:    2026,
		LastAction:   "NEW",
	}
	repo := &fakeNWSEventsRepository{
		byNaturalKey: map[string]*models.NWSEvent{
			naturalKey("O", "KAKQ", "SV", "W", "0273", 2026): existing,
		},
	}
	req := thundercall.IncomingMessageRequest{
		PrimaryVTECCount: 1,
		VTECProductClass: "O",
		VTECAction:       "CON",
		VTECOfficeID:     "KAKQ",
		VTECPhenomenon:   "SV",
		VTECSignificance: "W",
		VTECETN:          "0273",
		VTECBeginsAtRaw:  zeroVTECTimestamp,
		VTECEndsAtRaw:    "270101T0030Z",
		VTECEndsAt:       time.Date(2027, 1, 1, 0, 30, 0, 0, time.UTC),
		Timestamp:        time.Date(2027, 1, 1, 0, 5, 0, 0, time.UTC),
	}

	event, err := resolveNWSEvent(context.Background(), repo, req)
	if err != nil {
		t.Fatalf("resolveNWSEvent() error = %v", err)
	}
	if event == nil {
		t.Fatal("resolveNWSEvent() = nil, want event")
	}
	if event.ID != 99 {
		t.Fatalf("event.ID = %d, want 99", event.ID)
	}
	if repo.createCalls != 0 {
		t.Fatalf("createCalls = %d, want 0", repo.createCalls)
	}
	if repo.updateCalls != 1 {
		t.Fatalf("updateCalls = %d, want 1", repo.updateCalls)
	}
}

type fakeNWSEventsRepository struct {
	nextID       int64
	createCalls  int
	updateCalls  int
	byEventKey   map[string]*models.NWSEvent
	byNaturalKey map[string]*models.NWSEvent
}

func (r *fakeNWSEventsRepository) CreateOrGet(_ context.Context, event *models.NWSEvent) (bool, error) {
	if r.byEventKey == nil {
		r.byEventKey = make(map[string]*models.NWSEvent)
	}
	if r.byNaturalKey == nil {
		r.byNaturalKey = make(map[string]*models.NWSEvent)
	}
	if existing, ok := r.byEventKey[event.EventKey]; ok {
		*event = *existing
		return false, nil
	}

	r.createCalls++
	r.nextID++
	event.ID = r.nextID
	stored := *event
	r.byEventKey[event.EventKey] = &stored
	r.byNaturalKey[naturalKey(event.ProductClass, event.OfficeID, event.Phenomenon, event.Significance, event.ETN, event.EventYear)] = &stored
	return true, nil
}

func (r *fakeNWSEventsRepository) GetByNaturalKey(_ context.Context, productClass string, officeID string, phenomenon string, significance string, etn string, eventYear int) (*models.NWSEvent, error) {
	if r.byNaturalKey == nil {
		return nil, nil
	}
	existing := r.byNaturalKey[naturalKey(productClass, officeID, phenomenon, significance, etn, eventYear)]
	if existing == nil {
		return nil, nil
	}
	copy := *existing
	return &copy, nil
}

func (r *fakeNWSEventsRepository) UpdateLifecycle(_ context.Context, id int64, lastAction string, beginsAt *time.Time, endsAt *time.Time, issuedAt *time.Time) error {
	r.updateCalls++
	for _, event := range r.byEventKey {
		if event.ID != id {
			continue
		}
		event.LastAction = lastAction
		event.BeginsAt = beginsAt
		event.EndsAt = endsAt
		event.LastIssuedAt = issuedAt
		return nil
	}
	return nil
}

func naturalKey(productClass string, officeID string, phenomenon string, significance string, etn string, eventYear int) string {
	return buildNWSEventKey(productClass, officeID, phenomenon, significance, etn, eventYear)
}
