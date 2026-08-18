package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

type Channel string

const (
	ChannelSMS   Channel = "sms"
	ChannelEmail Channel = "email"
	ChannelVoice Channel = "voice"
)

func (c Channel) Valid() bool {
	switch c {
	case ChannelSMS, ChannelEmail, ChannelVoice:
		return true
	default:
		return false
	}
}

type StringSlice []string

func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}

	bytes, err := json.Marshal([]string(s))
	if err != nil {
		return nil, err
	}

	return string(bytes), nil
}

func (s *StringSlice) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*s = nil
		return nil
	case []byte:
		return s.fromBytes(v)
	case string:
		return s.fromBytes([]byte(v))
	default:
		return fmt.Errorf("unsupported StringSlice value type %T", value)
	}
}

func (s *StringSlice) fromBytes(value []byte) error {
	if len(value) == 0 {
		*s = nil
		return nil
	}

	var decoded []string
	if err := json.Unmarshal(value, &decoded); err != nil {
		return err
	}

	*s = StringSlice(decoded)
	return nil
}

type Account struct {
	ID        int64
	Name      string
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type User struct {
	ID          int64
	AccountID   int64
	ExternalID  *string
	FirstName   *string
	LastName    *string
	DisplayName *string
	Title       *string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type UserContactMethod struct {
	ID          int64
	UserID      int64
	Channel     Channel
	Destination string
	IsPrimary   bool
	IsVerified  bool
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Location struct {
	ID                   int64
	AccountID            int64
	Name                 string
	AddressLine1         *string
	AddressLine2         *string
	City                 *string
	StateCode            *string
	PostalCode           *string
	CountyFIPS           *string
	NWSZone              *string
	Latitude             *float64
	Longitude            *float64
	CoverageWKT          *string
	IsThunderCallEnabled bool
	Active               bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type UserLocation struct {
	ID                   int64
	UserID               int64
	LocationID           int64
	SubscriptionType     string
	IsPrimary            bool
	IsThunderCallEnabled bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type AccountSetting struct {
	ID              int64
	AccountID       int64
	MessageTypeCode string
	SMSEnabled      bool
	EmailEnabled    bool
	VoiceEnabled    bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type UserSetting struct {
	ID              int64
	UserID          int64
	MessageTypeCode string
	SMSEnabled      bool
	EmailEnabled    bool
	VoiceEnabled    bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type APIUser struct {
	ID           int64
	AccountID    int64
	Email        string
	PasswordHash string
	DisplayName  *string
	Active       bool
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type APISession struct {
	ID         int64
	APIUserID  int64
	TokenHash  string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastSeenAt *time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type NWSEvent struct {
	ID            int64
	EventKey      string
	ProductClass  string
	OfficeID      string
	Phenomenon    string
	Significance  string
	ETN           string
	EventYear     int
	LastAction    string
	BeginsAt      *time.Time
	EndsAt        *time.Time
	FirstIssuedAt *time.Time
	LastIssuedAt  *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type Message struct {
	ID                 int64
	AccountID          *int64
	SourceMessageID    *int64
	NWSEventID         *int64
	ExternalMessageID  *string
	SourceSegmentIndex *int
	Fingerprint        string
	Source             string
	EventCode          string
	MessageType        string
	AlertTypeCode      string
	Title              *string
	Body               string
	Coordinate         *string
	PolygonWKT         *string
	FIPSCodes          StringSlice
	NWSZones           StringSlice
	PrimaryVTECRaw     *string
	VTECAction         *string
	OriginalPayload    *string
	Status             string
	IssuedAt           *time.Time
	ReceivedAt         time.Time
	ProcessedAt        *time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type UserMessage struct {
	ID                int64
	MessageID         int64
	UserID            int64
	MatchedLocationID *int64
	ResolutionReason  string
	SMSEnabled        bool
	EmailEnabled      bool
	VoiceEnabled      bool
	Status            string
	QueuedAt          time.Time
	DeliveredAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type Notification struct {
	ID               int64
	NWSEventID       int64
	UserID           int64
	Channel          Channel
	FirstMessageID   int64
	LastMessageID    int64
	Status           string
	FirstAttemptedAt *time.Time
	SentAt           *time.Time
	DeliveredAt      *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type DeliveryAttempt struct {
	ID                int64
	UserMessageID     int64
	NotificationID    *int64
	Channel           Channel
	AttemptNumber     int
	Destination       string
	Provider          *string
	ProviderMessageID *string
	Status            string
	ErrorMessage      *string
	RequestedAt       time.Time
	DispatchAfter     time.Time
	LeaseToken        *string
	LeaseOwner        *string
	LeaseExpiresAt    *time.Time
	SentAt            *time.Time
	DeliveredAt       *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type SourceMessage struct {
	ID              int64
	Source          string
	ExternalID      string
	WMOCode         *string
	WFOCode         *string
	AWIPSID         *string
	ProductCategory *string
	IssuedAt        *time.Time
	RawPayload      string
	Status          string
	ParseError      *string
	ReceivedAt      time.Time
	ParsedAt        *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type OutboxEvent struct {
	ID            int64
	AggregateType string
	AggregateID   int64
	EventType     string
	StreamKey     string
	PayloadJSON   string
	PublishedAt   *time.Time
	AttemptCount  int
	LastError     *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
