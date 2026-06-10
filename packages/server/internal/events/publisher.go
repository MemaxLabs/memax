package events

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Publisher interface {
	Publish(ctx context.Context, evt Event) error
	Close() error
}

type RedisPublisher struct {
	client  *redis.Client
	channel string
}

func NewPublisherFromEnv() Publisher {
	url := os.Getenv("REDIS_URL")
	if url == "" {
		return nil
	}
	return NewPublisher(url)
}

func NewPublisher(url string) Publisher {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil
	}
	return &RedisPublisher{
		client:  redis.NewClient(opts),
		channel: "memax:events",
	}
}

func (p *RedisPublisher) Publish(ctx context.Context, evt Event) error {
	if p == nil || p.client == nil {
		return nil
	}
	if evt.ID == "" {
		evt.ID = uuid.NewString()
	}
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = timeNow()
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return p.client.Publish(ctx, p.channel, payload).Err()
}

func (p *RedisPublisher) Close() error {
	if p == nil || p.client == nil {
		return nil
	}
	return p.client.Close()
}

var timeNow = func() time.Time {
	return time.Now()
}
