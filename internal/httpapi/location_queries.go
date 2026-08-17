package httpapi

import (
	"context"
	"database/sql"
	"strings"

	"thundercall-go/internal/geocode"
	"thundercall-go/internal/models"
	"thundercall-go/internal/repositories/sqlutil"
)

func (s *Server) lookupMessagesByLocation(ctx context.Context, resolved geocode.ResolvedLocation, limit int, offset int) ([]locationMessageLookupItem, int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id,
			source,
			event_code,
			message_type,
			alert_type_code,
			title,
			status,
			issued_at,
			received_at,
			processed_at,
			source_message_id,
			external_message_id,
			source_segment_index,
			polygon_wkt,
			fips_codes_json,
			nws_zones_json
		FROM messages
		WHERE polygon_wkt IS NOT NULL
		   OR fips_codes_json IS NOT NULL
		   OR nws_zones_json IS NOT NULL
		ORDER BY COALESCE(issued_at, received_at) DESC, id DESC`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	all := make([]locationMessageLookupItem, 0, limit)
	for rows.Next() {
		item, err := scanLocationMessageLookupItem(rows)
		if err != nil {
			return nil, 0, err
		}

		reasons := locationMatchReasons(item, resolved)
		if len(reasons) == 0 {
			continue
		}
		item.MatchReasons = reasons
		all = append(all, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	total := int64(len(all))
	if offset >= len(all) {
		return []locationMessageLookupItem{}, total, nil
	}

	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], total, nil
}

func locationMatchReasons(item locationMessageLookupItem, resolved geocode.ResolvedLocation) []string {
	reasons := make([]string, 0, 3)

	if containsFold(item.FIPSCodes, resolved.CountyFIPS) {
		reasons = append(reasons, "countyFips")
	}
	if containsFold(item.NWSZones, resolved.NWSZone) {
		reasons = append(reasons, "nwsZone")
	}
	if item.PolygonWKT != nil && resolved.Latitude != 0 && resolved.Longitude != 0 {
		inside, err := geocode.PolygonContainsPoint(*item.PolygonWKT, resolved.Latitude, resolved.Longitude)
		if err == nil && inside {
			reasons = append(reasons, "polygon")
		}
	}

	return reasons
}

func containsFold(values []string, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), target) {
			return true
		}
	}
	return false
}

func scanLocationMessageLookupItem(s scanner) (locationMessageLookupItem, error) {
	var (
		item               locationMessageLookupItem
		title              sql.NullString
		issuedAt           sql.NullTime
		processedAt        sql.NullTime
		sourceMessageID    sql.NullInt64
		externalMessageID  sql.NullString
		sourceSegmentIndex sql.NullInt64
		polygonWKT         sql.NullString
		fipsCodes          models.StringSlice
		nwsZones           models.StringSlice
	)

	err := s.Scan(
		&item.ID,
		&item.Source,
		&item.EventCode,
		&item.MessageType,
		&item.AlertTypeCode,
		&title,
		&item.Status,
		&issuedAt,
		&item.ReceivedAt,
		&processedAt,
		&sourceMessageID,
		&externalMessageID,
		&sourceSegmentIndex,
		&polygonWKT,
		&fipsCodes,
		&nwsZones,
	)
	if err != nil {
		return locationMessageLookupItem{}, err
	}

	item.Title = sqlutil.StringPtr(title)
	item.IssuedAt = sqlutil.TimePtr(issuedAt)
	item.ProcessedAt = sqlutil.TimePtr(processedAt)
	item.SourceMessageID = sqlutil.Int64Ptr(sourceMessageID)
	item.ExternalMessageID = sqlutil.StringPtr(externalMessageID)
	item.SourceSegmentIndex = sqlutil.IntPtr[int](sourceSegmentIndex)
	item.PolygonWKT = sqlutil.StringPtr(polygonWKT)
	item.FIPSCodes = []string(fipsCodes)
	item.NWSZones = []string(nwsZones)
	return item, nil
}
