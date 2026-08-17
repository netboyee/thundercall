package nwws

import "time"

const zeroVTECTimestamp = "000000T0000Z"

type StanzaEnvelope struct {
	CCCCode    string
	WMOCode    string
	IssueTime  time.Time
	AWIPSID    string
	ExternalID string
	Body       string
}

type WMOHeader struct {
	DataType      string
	IssuingOffice string
	BBBDesignator string
	IssuedAt      time.Time
}

type AWIPSIdentifier struct {
	ProductCategory   string
	OriginatingOffice string
}

type MNDHeader struct {
	BroadcastInstruction string
	ProductName          string
	IssuingOffice        string
	IssuingOfficeLines   []string
	IssuanceDateTime     string
}

type UGCCode struct {
	Format    string
	State     string
	Code      string
	ExpiresAt time.Time
}

type PrimaryVTEC struct {
	Raw          string
	ProductClass string
	Action       string
	OfficeID     string
	Phenomenon   string
	Significance string
	ETN          string
	BeginsAtRaw  string
	EndsAtRaw    string
	BeginsAt     time.Time
	EndsAt       time.Time
}

func (v PrimaryVTEC) EventCode() string {
	return v.Phenomenon + v.Significance
}

func (v PrimaryVTEC) HasZeroBeginTime() bool {
	return v.BeginsAtRaw == zeroVTECTimestamp
}

func (v PrimaryVTEC) HasZeroEndTime() bool {
	return v.EndsAtRaw == zeroVTECTimestamp
}

type HydrologicVTEC struct {
	NWSLocationIdentifier string
	FloodSeverity         string
	ImmediateCause        string
	BeginsAtRaw           string
	CrestAtRaw            string
	EndsAtRaw             string
	BeginsAt              time.Time
	CrestAt               time.Time
	EndsAt                time.Time
	FloodRecord           string
}

func (v HydrologicVTEC) HasZeroBeginTime() bool {
	return v.BeginsAtRaw == zeroVTECTimestamp
}

func (v HydrologicVTEC) HasZeroCrestTime() bool {
	return v.CrestAtRaw == zeroVTECTimestamp
}

func (v HydrologicVTEC) HasZeroEndTime() bool {
	return v.EndsAtRaw == zeroVTECTimestamp
}

type VTECPair struct {
	Primary    PrimaryVTEC
	Hydrologic *HydrologicVTEC
}

type SegmentHeader struct {
	UGCCodes                 []UGCCode
	PrimaryVTEC              PrimaryVTEC
	HydrologicVTEC           HydrologicVTEC
	PrimaryVTECs             []PrimaryVTEC
	HydrologicVTECs          []HydrologicVTEC
	VTECPairs                []VTECPair
	PlainLanguageGeographies string
	IncludingCitiesOf        string
	IssuanceDateTime         string
}

type Coordinate struct {
	Latitude  float64
	Longitude float64
}

type Segment struct {
	Header  SegmentHeader
	Message string
	Polygon []Coordinate
}

type ParsedMessage struct {
	WMOHeader               WMOHeader
	AWIPSIdentifier         AWIPSIdentifier
	MNDHeader               MNDHeader
	ProductHeadlineOverview string
	Segments                []Segment
	Footer                  string
	Original                string
}
