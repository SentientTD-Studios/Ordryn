package statestore

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"GoTodo/internal/config"

	"github.com/redis/go-redis/v9"
)

// redisChannelPrefix namespaces live-update channels.
const redisChannelPrefix = "gotodo:sse:"

type redisStore struct {
	client *redis.Client
}

// newRedisStore connects using REDIS_URL (redis:// or rediss://), or
// REDIS_ADDR + REDIS_PASSWORD when REDIS_URL is unset.
func newRedisStore(ctx context.Context) (*redisStore, error) {
	var opt *redis.Options
	if u := strings.TrimSpace(os.Getenv("REDIS_URL")); u != "" {
		var err error
		if opt, err = redis.ParseURL(u); err != nil {
			return nil, fmt.Errorf("invalid REDIS_URL: %w", err)
		}
	} else if addr := strings.TrimSpace(os.Getenv("REDIS_ADDR")); addr != "" {
		opt = &redis.Options{Addr: addr, Password: os.Getenv("REDIS_PASSWORD")}
	} else {
		return nil, errors.New("REDIS_URL or REDIS_ADDR is required")
	}
	client := redis.NewClient(opt)
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping failed: %w", err)
	}
	return &redisStore{client: client}, nil
}

func (s *redisStore) Backend() string { return config.StateStoreRedis }

func (s *redisStore) Ping(ctx context.Context) error { return s.client.Ping(ctx).Err() }

func (s *redisStore) Close() { _ = s.client.Close() }

func redisResult(b []byte, err error) ([]byte, error) {
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	return b, err
}

func (s *redisStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return s.client.Set(ctx, key, value, ttl).Err()
}

func (s *redisStore) SetMany(ctx context.Context, values map[string][]byte, ttl time.Duration) error {
	pipe := s.client.TxPipeline()
	for k, v := range values {
		pipe.Set(ctx, k, v, ttl)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func (s *redisStore) Get(ctx context.Context, key string) ([]byte, error) {
	return redisResult(s.client.Get(ctx, key).Bytes())
}

func (s *redisStore) GetDel(ctx context.Context, key string) ([]byte, error) {
	return redisResult(s.client.GetDel(ctx, key).Bytes())
}

const compareAndSetScript = `if redis.call("GET", KEYS[1]) == ARGV[1] then
  redis.call("SET", KEYS[1], ARGV[2], "PX", ARGV[3])
  return 1
end
return 0`

func (s *redisStore) CompareAndSet(ctx context.Context, key string, old, value []byte, ttl time.Duration) (bool, error) {
	n, err := s.client.Eval(ctx, compareAndSetScript, []string{key}, old, value, ttl.Milliseconds()).Int64()
	return n == 1, err
}

func (s *redisStore) Del(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

func (s *redisStore) Exists(ctx context.Context, key string) (bool, error) {
	n, err := s.client.Exists(ctx, key).Result()
	return n > 0, err
}

func (s *redisStore) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := s.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		_ = s.client.Expire(ctx, key, ttl).Err()
	}
	return n, nil
}

// tokenBucketScript implements a token bucket in a Redis hash.
const tokenBucketScript = `local key = KEYS[1]
local now = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local capacity = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

local data = redis.call("HMGET", key, "tokens", "last")
local tokens = data[1]
local last = data[2]
if tokens == false or tokens == nil then
  tokens = capacity
  last = now
else
  tokens = tonumber(tokens)
  last = tonumber(last)
end
local delta = math.max(0, now - last)
local add = delta * refill_rate
tokens = math.min(capacity, tokens + add)
if tokens < requested then
  redis.call("HMSET", key, "tokens", tokens, "last", now)
  redis.call("EXPIRE", key, ttl)
  return 0
else
  tokens = tokens - requested
  redis.call("HMSET", key, "tokens", tokens, "last", now)
  redis.call("EXPIRE", key, ttl)
  return 1
end`

func (s *redisStore) Allow(ctx context.Context, key string, capacity int, refillPerSec float64, ttl time.Duration) (bool, error) {
	res, err := s.client.Eval(ctx, tokenBucketScript, []string{key},
		time.Now().Unix(), refillPerSec, capacity, 1, int64(ttl.Seconds())).Result()
	if err != nil {
		return false, fmt.Errorf("redis eval error: %w", err)
	}
	allowed, ok := res.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected redis response: %T", res)
	}
	return allowed == 1, nil
}

func (s *redisStore) Publish(ctx context.Context, channel string, payload []byte) error {
	return s.client.Publish(ctx, redisChannelPrefix+channel, payload).Err()
}

func (s *redisStore) Subscribe(ctx context.Context, handler func(channel string, payload []byte)) {
	go func() {
		pubsub := s.client.PSubscribe(ctx, redisChannelPrefix+"*")
		defer pubsub.Close()
		for msg := range pubsub.Channel() {
			if msg == nil {
				continue
			}
			handler(strings.TrimPrefix(msg.Channel, redisChannelPrefix), []byte(msg.Payload))
		}
		if ctx.Err() == nil {
			log.Printf("redis live-update subscription ended")
		}
	}()
}
