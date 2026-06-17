package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type StateStore struct {
	client *redis.Client
	prefix string
}

func NewStateStore(addr, prefix string) *StateStore {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &StateStore{
		client: client,
		prefix: prefix,
	}
}

func (s *StateStore) NewClient(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr: addr,
	})
}

func (s *StateStore) key(gameID string) string {
	return fmt.Sprintf("%s:game:%s", s.prefix, gameID)
}

func (s *StateStore) actionKey(gameID string) string {
	return fmt.Sprintf("%s:game:%s:actions", s.prefix, gameID)
}

func (s *StateStore) SaveGameState(ctx context.Context, gameID string, state interface{}) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.key(gameID), data, 24*time.Hour).Err()
}

func (s *StateStore) GetGameState(ctx context.Context, gameID string, state interface{}) error {
	data, err := s.client.Get(ctx, s.key(gameID)).Result()
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(data), state)
}

func (s *StateStore) DeleteGameState(ctx context.Context, gameID string) error {
	return s.client.Del(ctx, s.key(gameID)).Err()
}

func (s *StateStore) AppendAction(ctx context.Context, gameID string, action interface{}) error {
	data, err := json.Marshal(action)
	if err != nil {
		return err
	}
	return s.client.RPush(ctx, s.actionKey(gameID), data).Err()
}

func (s *StateStore) GetActions(ctx context.Context, gameID string, start, end int64) ([][]byte, error) {
	return s.client.LRange(ctx, s.actionKey(gameID), start, end).Result()
}

func (s *StateStore) TrimActions(ctx context.Context, gameID string, keep int64) error {
	return s.client.LTrim(ctx, s.actionKey(gameID), -keep, -1).Err()
}

func (s *StateStore) Publish(ctx context.Context, channel string, message interface{}) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return s.client.Publish(ctx, channel, data).Err()
}

func (s *StateStore) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	return s.client.Subscribe(ctx, channels...)
}

func (s *StateStore) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) bool {
	return s.client.SetNX(ctx, key, value, expiration).Val()
}

func (s *StateStore) Get(ctx context.Context, key string) (string, error) {
	return s.client.Get(ctx, key).Result()
}

func (s *StateStore) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return s.client.Set(ctx, key, value, expiration).Err()
}

func (s *StateStore) Del(ctx context.Context, keys ...string) error {
	return s.client.Del(ctx, keys...).Err()
}

func (s *StateStore) Exists(ctx context.Context, keys ...string) int64 {
	return s.client.Exists(ctx, keys...).Val()
}

func (s *StateStore) HSet(ctx context.Context, key string, values ...interface{}) error {
	return s.client.HSet(ctx, key, values...).Err()
}

func (s *StateStore) HGet(ctx context.Context, key, field string) (string, error) {
	return s.client.HGet(ctx, key, field).Result()
}

func (s *StateStore) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return s.client.HGetAll(ctx, key).Result()
}

func (s *StateStore) ZAdd(ctx context.Context, key string, score float64, member interface{}) error {
	return s.client.ZAdd(ctx, key, redis.Z{Score: score, Member: member}).Err()
}

func (s *StateStore) ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return s.client.ZRange(ctx, key, start, stop).Result()
}

func (s *StateStore) ZRem(ctx context.Context, key string, members ...interface{}) error {
	return s.client.ZRem(ctx, key, members...).Err()
}

func (s *StateStore) Close() error {
	return s.client.Close()
}

func (s *StateStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}
