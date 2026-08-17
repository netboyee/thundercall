package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	MySQL     MySQLConfig
	Redis     RedisConfig
	NWWS      NWWSConfig
	Ingest    IngestConfig
	Worker    WorkerConfig
	API       APIConfig
	Geocoding GeocodingConfig
	Twilio    TwilioConfig
	SendGrid  SendGridConfig
}

type MySQLConfig struct {
	DSN string
}

func (c MySQLConfig) Enabled() bool {
	return c.DSN != ""
}

type RedisConfig struct {
	Addr          string
	Password      string
	DB            int
	StreamKey     string
	ConsumerGroup string
	ConsumerName  string
	Block         time.Duration
	ClaimMinIdle  time.Duration
	BatchSize     int64
}

func (c RedisConfig) Enabled() bool {
	return c.Addr != "" && c.StreamKey != "" && c.ConsumerGroup != ""
}

type NWWSConfig struct {
	Domain          string
	RoomServer      string
	Room            string
	Username        string
	Password        string
	JoinPassword    string
	Nickname        string
	Products        []string
	LogFullMessages bool
	IdleTimeout     time.Duration
}

func (c NWWSConfig) Enabled() bool {
	return c.Username != "" && c.Password != ""
}

func (c NWWSConfig) RoomJID() string {
	return c.Room + "@" + c.RoomServer
}

func (c NWWSConfig) Nick() string {
	if c.Nickname != "" {
		return c.Nickname
	}
	return c.Username
}

type IngestConfig struct {
	PublishInterval  time.Duration
	PublishBatchSize int
}

type WorkerConfig struct {
	ReadCount int64
}

type APIConfig struct {
	ListenAddr string
	SessionTTL time.Duration
}

type GeocodingConfig struct {
	CensusBaseURL     string
	CensusBenchmark   string
	CensusVintage     string
	WeatherGovBaseURL string
	UserAgent         string
	Timeout           time.Duration
}

type TwilioConfig struct {
	AccountSID          string
	AuthToken           string
	MessagingServiceSID string
	SMSFrom             string
	VoiceFrom           string
	VoiceStatusCallback string
	VoiceLogOnly        bool
}

func (c TwilioConfig) Enabled() bool {
	return c.AccountSID != "" && c.AuthToken != ""
}

type SendGridConfig struct {
	APIKey    string
	FromEmail string
	FromName  string
}

func (c SendGridConfig) Enabled() bool {
	return c.APIKey != "" && c.FromEmail != ""
}

func Load() (Config, error) {
	cfg := Config{
		MySQL: MySQLConfig{
			DSN: os.Getenv("THUNDERCALL_MYSQL_DSN"),
		},
		Redis: RedisConfig{
			Addr:          os.Getenv("THUNDERCALL_REDIS_ADDR"),
			Password:      os.Getenv("THUNDERCALL_REDIS_PASSWORD"),
			DB:            intValueOrDefault("THUNDERCALL_REDIS_DB", 0),
			StreamKey:     valueOrDefault("THUNDERCALL_REDIS_STREAM", "thundercall:messages"),
			ConsumerGroup: valueOrDefault("THUNDERCALL_REDIS_GROUP", "thundercall-workers"),
			ConsumerName:  valueOrDefault("THUNDERCALL_REDIS_CONSUMER", hostnameOr("worker")),
			Block:         durationValueOrDefault("THUNDERCALL_REDIS_BLOCK", 5*time.Second),
			ClaimMinIdle:  durationValueOrDefault("THUNDERCALL_REDIS_CLAIM_IDLE", 30*time.Second),
			BatchSize:     int64ValueOrDefault("THUNDERCALL_REDIS_BATCH_SIZE", 25),
		},
		NWWS: NWWSConfig{
			Domain:          valueOrDefault("THUNDERCALL_NWWS_DOMAIN", "nwws-oi.weather.gov"),
			RoomServer:      valueOrDefault("THUNDERCALL_NWWS_ROOM_SERVER", "conference.nwws-oi.weather.gov"),
			Room:            valueOrDefault("THUNDERCALL_NWWS_ROOM", "nwws"),
			Username:        os.Getenv("THUNDERCALL_NWWS_USERNAME"),
			Password:        os.Getenv("THUNDERCALL_NWWS_PASSWORD"),
			JoinPassword:    os.Getenv("THUNDERCALL_NWWS_JOIN_PASSWORD"),
			Nickname:        os.Getenv("THUNDERCALL_NWWS_NICK"),
			Products:        csvValueOrDefault("THUNDERCALL_NWWS_PRODUCTS", []string{"SVR", "FFW", "TOR", "WSW", "TSU", "NPW"}),
			LogFullMessages: boolValueOrDefault("THUNDERCALL_NWWS_LOG_FULL_MESSAGES", false),
			IdleTimeout:     durationValueOrDefault("THUNDERCALL_NWWS_IDLE_TIMEOUT", 5*time.Minute),
		},
		Ingest: IngestConfig{
			PublishInterval:  durationValueOrDefault("THUNDERCALL_INGEST_PUBLISH_INTERVAL", 2*time.Second),
			PublishBatchSize: intValueOrDefault("THUNDERCALL_INGEST_PUBLISH_BATCH_SIZE", 50),
		},
		Worker: WorkerConfig{
			ReadCount: int64ValueOrDefault("THUNDERCALL_WORKER_READ_COUNT", 25),
		},
		API: APIConfig{
			ListenAddr: valueOrDefault("THUNDERCALL_API_LISTEN_ADDR", ":8080"),
			SessionTTL: durationValueOrDefault("THUNDERCALL_API_SESSION_TTL", 24*time.Hour),
		},
		Geocoding: GeocodingConfig{
			CensusBaseURL:     valueOrDefault("THUNDERCALL_CENSUS_BASE_URL", "https://geocoding.geo.census.gov/geocoder"),
			CensusBenchmark:   valueOrDefault("THUNDERCALL_CENSUS_BENCHMARK", "Public_AR_Current"),
			CensusVintage:     valueOrDefault("THUNDERCALL_CENSUS_VINTAGE", "Current_Current"),
			WeatherGovBaseURL: valueOrDefault("THUNDERCALL_WEATHERGOV_BASE_URL", "https://api.weather.gov"),
			UserAgent:         valueOrDefault("THUNDERCALL_GEOCODER_USER_AGENT", "thundercall/0.1"),
			Timeout:           durationValueOrDefault("THUNDERCALL_GEOCODER_TIMEOUT", 10*time.Second),
		},
		Twilio: TwilioConfig{
			AccountSID:          os.Getenv("TWILIO_ACCOUNT_SID"),
			AuthToken:           os.Getenv("TWILIO_AUTH_TOKEN"),
			MessagingServiceSID: os.Getenv("TWILIO_MESSAGING_SERVICE_SID"),
			SMSFrom:             os.Getenv("TWILIO_SMS_FROM"),
			VoiceFrom:           os.Getenv("TWILIO_VOICE_FROM"),
			VoiceStatusCallback: os.Getenv("TWILIO_VOICE_STATUS_CALLBACK"),
			VoiceLogOnly:        boolValueOrDefault("THUNDERCALL_TWILIO_VOICE_LOG_ONLY", true),
		},
		SendGrid: SendGridConfig{
			APIKey:    os.Getenv("SENDGRID_API_KEY"),
			FromEmail: os.Getenv("SENDGRID_FROM_EMAIL"),
			FromName:  os.Getenv("SENDGRID_FROM_NAME"),
		},
	}
	if cfg.NWWS.JoinPassword == "" {
		cfg.NWWS.JoinPassword = cfg.NWWS.Password
	}

	return cfg, nil
}

func valueOrDefault(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationValueOrDefault(key string, fallback time.Duration) time.Duration {
	if raw := os.Getenv(key); raw != "" {
		if value, err := time.ParseDuration(raw); err == nil {
			return value
		}
	}
	return fallback
}

func intValueOrDefault(key string, fallback int) int {
	if raw := os.Getenv(key); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			return value
		}
	}
	return fallback
}

func int64ValueOrDefault(key string, fallback int64) int64 {
	if raw := os.Getenv(key); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return value
		}
	}
	return fallback
}

func boolValueOrDefault(key string, fallback bool) bool {
	if raw := os.Getenv(key); raw != "" {
		if value, err := strconv.ParseBool(raw); err == nil {
			return value
		}
	}
	return fallback
}

func csvValueOrDefault(key string, fallback []string) []string {
	raw := os.Getenv(key)
	if strings.TrimSpace(raw) == "" {
		return normalizedCSVValues(fallback)
	}

	values := strings.Split(raw, ",")
	out := normalizedCSVValues(values)
	if len(out) == 0 {
		return normalizedCSVValues(fallback)
	}
	return out
}

func normalizedCSVValues(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.ToUpper(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func hostnameOr(fallback string) string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}
