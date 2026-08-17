package redisstreams

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"thundercall-go/internal/config"
)

type Client struct {
	raw *redis.Client
	cfg config.RedisConfig
}

type StreamMessage struct {
	ID            string
	EventType     string
	AggregateType string
	AggregateID   int64
	Payload       string
}

func New(cfg config.RedisConfig) *Client {
	return &Client{
		raw: redis.NewClient(&redis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		}),
		cfg: cfg,
	}
}

func (c *Client) Close() error {
	if c == nil || c.raw == nil {
		return nil
	}
	return c.raw.Close()
}

func (c *Client) Ping(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return fmt.Errorf("redis client is not configured")
	}
	return c.raw.Ping(ctx).Err()
}

func (c *Client) EnsureGroup(ctx context.Context) error {
	if c == nil || c.raw == nil {
		return fmt.Errorf("redis client is not configured")
	}

	err := c.raw.XGroupCreateMkStream(ctx, c.cfg.StreamKey, c.cfg.ConsumerGroup, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func (c *Client) Publish(ctx context.Context, streamKey string, eventType string, aggregateType string, aggregateID int64, payload string) (string, error) {
	if c == nil || c.raw == nil {
		return "", fmt.Errorf("redis client is not configured")
	}

	return c.raw.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey,
		Values: map[string]any{
			"event_type":     eventType,
			"aggregate_type": aggregateType,
			"aggregate_id":   aggregateID,
			"payload":        payload,
		},
	}).Result()
}

func (c *Client) AutoClaim(ctx context.Context, start string, count int64) ([]StreamMessage, string, error) {
	if c == nil || c.raw == nil {
		return nil, start, fmt.Errorf("redis client is not configured")
	}
	if start == "" {
		start = "0-0"
	}

	messages, nextStart, err := c.raw.XAutoClaim(ctx, &redis.XAutoClaimArgs{
		Stream:   c.cfg.StreamKey,
		Group:    c.cfg.ConsumerGroup,
		Consumer: c.cfg.ConsumerName,
		Start:    start,
		MinIdle:  c.cfg.ClaimMinIdle,
		Count:    count,
	}).Result()
	if err == redis.Nil {
		return nil, nextStart, nil
	}
	if err != nil {
		return nil, nextStart, err
	}

	out := make([]StreamMessage, 0, len(messages))
	for _, message := range messages {
		parsed, parseErr := parseMessage(message)
		if parseErr != nil {
			return nil, nextStart, parseErr
		}
		out = append(out, parsed)
	}
	return out, nextStart, nil
}

func (c *Client) ReadGroup(ctx context.Context, count int64, block time.Duration) ([]StreamMessage, error) {
	if c == nil || c.raw == nil {
		return nil, fmt.Errorf("redis client is not configured")
	}

	streams, err := c.raw.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.cfg.ConsumerGroup,
		Consumer: c.cfg.ConsumerName,
		Streams:  []string{c.cfg.StreamKey, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []StreamMessage
	for _, stream := range streams {
		for _, message := range stream.Messages {
			parsed, parseErr := parseMessage(message)
			if parseErr != nil {
				return nil, parseErr
			}
			out = append(out, parsed)
		}
	}
	return out, nil
}

func (c *Client) Ack(ctx context.Context, ids ...string) error {
	if c == nil || c.raw == nil {
		return fmt.Errorf("redis client is not configured")
	}
	if len(ids) == 0 {
		return nil
	}
	return c.raw.XAck(ctx, c.cfg.StreamKey, c.cfg.ConsumerGroup, ids...).Err()
}

func parseMessage(message redis.XMessage) (StreamMessage, error) {
	eventType := valueAsString(message.Values["event_type"])
	aggregateType := valueAsString(message.Values["aggregate_type"])
	payload := valueAsString(message.Values["payload"])
	aggregateID, err := valueAsInt64(message.Values["aggregate_id"])
	if err != nil {
		return StreamMessage{}, err
	}

	return StreamMessage{
		ID:            message.ID,
		EventType:     eventType,
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		Payload:       payload,
	}, nil
}

func valueAsString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func valueAsInt64(value any) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case string:
		return strconv.ParseInt(v, 10, 64)
	case []byte:
		return strconv.ParseInt(string(v), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported aggregate id type %T", value)
	}
}
