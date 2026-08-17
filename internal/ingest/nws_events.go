package ingest

import (
	"context"
	"fmt"
	"strings"
	"time"

	"thundercall-go/internal/models"
	"thundercall-go/internal/thundercall"
)

const zeroVTECTimestamp = "000000T0000Z"

type nwsEventsRepository interface {
	CreateOrGet(ctx context.Context, event *models.NWSEvent) (bool, error)
	GetByNaturalKey(ctx context.Context, productClass string, officeID string, phenomenon string, significance string, etn string, eventYear int) (*models.NWSEvent, error)
	UpdateLifecycle(ctx context.Context, id int64, lastAction string, beginsAt *time.Time, endsAt *time.Time, issuedAt *time.Time) error
}

func resolveNWSEvent(ctx context.Context, repo nwsEventsRepository, req thundercall.IncomingMessageRequest) (*models.NWSEvent, error) {
	if repo == nil || !req.HasSinglePrimaryVTEC() {
		return nil, nil
	}

	eventYear := eventYearFromRequest(req)
	if eventYear > 0 {
		event := newNWSEvent(req, eventYear)
		created, err := repo.CreateOrGet(ctx, event)
		if err != nil {
			return nil, err
		}
		if !created {
			if err := repo.UpdateLifecycle(ctx, event.ID, event.LastAction, event.BeginsAt, event.EndsAt, event.LastIssuedAt); err != nil {
				return nil, err
			}
		}
		return event, nil
	}

	for _, candidateYear := range candidateEventYears(req.Timestamp) {
		existing, err := repo.GetByNaturalKey(
			ctx,
			normalizeVTECField(req.VTECProductClass),
			normalizeVTECField(req.VTECOfficeID),
			normalizeVTECField(req.VTECPhenomenon),
			normalizeVTECField(req.VTECSignificance),
			normalizeVTECField(req.VTECETN),
			candidateYear,
		)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			continue
		}
		if err := repo.UpdateLifecycle(ctx, existing.ID, normalizeVTECField(req.VTECAction), timePointerIfValid(req.VTECBeginsAt, req.VTECBeginsAtRaw), timePointerIfValid(req.VTECEndsAt, req.VTECEndsAtRaw), timePointer(req.Timestamp)); err != nil {
			return nil, err
		}
		return existing, nil
	}

	event := newNWSEvent(req, candidateEventYears(req.Timestamp)[0])
	created, err := repo.CreateOrGet(ctx, event)
	if err != nil {
		return nil, err
	}
	if !created {
		if err := repo.UpdateLifecycle(ctx, event.ID, event.LastAction, event.BeginsAt, event.EndsAt, event.LastIssuedAt); err != nil {
			return nil, err
		}
	}
	return event, nil
}

func newNWSEvent(req thundercall.IncomingMessageRequest, eventYear int) *models.NWSEvent {
	productClass := normalizeVTECField(req.VTECProductClass)
	officeID := normalizeVTECField(req.VTECOfficeID)
	phenomenon := normalizeVTECField(req.VTECPhenomenon)
	significance := normalizeVTECField(req.VTECSignificance)
	etn := normalizeVTECField(req.VTECETN)

	return &models.NWSEvent{
		EventKey:      buildNWSEventKey(productClass, officeID, phenomenon, significance, etn, eventYear),
		ProductClass:  productClass,
		OfficeID:      officeID,
		Phenomenon:    phenomenon,
		Significance:  significance,
		ETN:           etn,
		EventYear:     eventYear,
		LastAction:    normalizeVTECField(req.VTECAction),
		BeginsAt:      timePointerIfValid(req.VTECBeginsAt, req.VTECBeginsAtRaw),
		EndsAt:        timePointerIfValid(req.VTECEndsAt, req.VTECEndsAtRaw),
		FirstIssuedAt: timePointer(req.Timestamp),
		LastIssuedAt:  timePointer(req.Timestamp),
	}
}

func buildNWSEventKey(productClass string, officeID string, phenomenon string, significance string, etn string, eventYear int) string {
	return fmt.Sprintf(
		"%s:%s:%s:%s:%s:%04d",
		normalizeVTECField(productClass),
		normalizeVTECField(officeID),
		normalizeVTECField(phenomenon),
		normalizeVTECField(significance),
		normalizeVTECField(etn),
		eventYear,
	)
}

func eventYearFromRequest(req thundercall.IncomingMessageRequest) int {
	if strings.EqualFold(strings.TrimSpace(req.VTECBeginsAtRaw), zeroVTECTimestamp) {
		return 0
	}
	if req.VTECBeginsAt.IsZero() {
		return 0
	}
	return req.VTECBeginsAt.UTC().Year()
}

func candidateEventYears(issuedAt time.Time) []int {
	reference := issuedAt.UTC()
	if reference.IsZero() {
		reference = time.Now().UTC()
	}
	return []int{reference.Year(), reference.Year() - 1}
}

func timePointerIfValid(value time.Time, raw string) *time.Time {
	if strings.EqualFold(strings.TrimSpace(raw), zeroVTECTimestamp) {
		return nil
	}
	return timePointer(value)
}

func normalizeVTECField(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}
