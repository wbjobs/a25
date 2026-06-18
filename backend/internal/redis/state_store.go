package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	pb "github.com/ecscard/game/internal/proto"
	"google.golang.org/protobuf/proto"
)

const (
	defaultTTL = 24 * time.Hour

	keyPrefixGameState = "game:state:"
	keyPrefixSnapshot  = "game:snapshot:"
	keyPrefixActions   = "game:actions:"
)

type StateStore struct {
	client     redis.UniversalClient
	prefix     string
	useCluster bool
}

type StoreOptions struct {
	Addrs        []string
	Password     string
	DB           int
	Prefix       string
	UseCluster   bool
	UseSentinel  bool
	MasterName   string
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolSize     int
}

func NewStateStore(addr, prefix string) *StateStore {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})
	return &StateStore{
		client:     client,
		prefix:     prefix,
		useCluster: false,
	}
}

func NewStateStoreCluster(addrs []string, password, prefix string) *StateStore {
	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:    addrs,
		Password: password,
	})
	return &StateStore{
		client:     client,
		prefix:     prefix,
		useCluster: true,
	}
}

func NewStateStoreWithOptions(opts StoreOptions) *StateStore {
	var client redis.UniversalClient

	if opts.UseCluster {
		client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        opts.Addrs,
			Password:     opts.Password,
			DialTimeout:  opts.DialTimeout,
			ReadTimeout:  opts.ReadTimeout,
			WriteTimeout: opts.WriteTimeout,
			PoolSize:     opts.PoolSize,
		})
	} else if opts.UseSentinel {
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    opts.MasterName,
			SentinelAddrs: opts.Addrs,
			Password:      opts.Password,
			DB:            opts.DB,
			DialTimeout:   opts.DialTimeout,
			ReadTimeout:   opts.ReadTimeout,
			WriteTimeout:  opts.WriteTimeout,
			PoolSize:      opts.PoolSize,
		})
	} else {
		var addr string
		if len(opts.Addrs) > 0 {
			addr = opts.Addrs[0]
		}
		client = redis.NewClient(&redis.Options{
			Addr:         addr,
			Password:     opts.Password,
			DB:           opts.DB,
			DialTimeout:  opts.DialTimeout,
			ReadTimeout:  opts.ReadTimeout,
			WriteTimeout: opts.WriteTimeout,
			PoolSize:     opts.PoolSize,
		})
	}

	return &StateStore{
		client:     client,
		prefix:     opts.Prefix,
		useCluster: opts.UseCluster,
	}
}

func (s *StateStore) NewClient(addr string) redis.UniversalClient {
	return redis.NewClient(&redis.Options{
		Addr: addr,
	})
}

func (s *StateStore) Client() redis.UniversalClient {
	return s.client
}

func (s *StateStore) IsCluster() bool {
	return s.useCluster
}

func (s *StateStore) buildKey(key string) string {
	return fmt.Sprintf("%s%s", s.prefix, key)
}

func (s *StateStore) gameStateKey(gameID string) string {
	return s.buildKey(keyPrefixGameState + gameID)
}

func (s *StateStore) actionKey(gameID string) string {
	return s.buildKey(keyPrefixActions + gameID)
}

func (s *StateStore) snapshotKey(gameID string, frame uint64) string {
	return s.buildKey(fmt.Sprintf("%s%s:%d", keyPrefixSnapshot, gameID, frame))
}

func (s *StateStore) snapshotPattern(gameID string) string {
	return s.buildKey(fmt.Sprintf("%s%s:*", keyPrefixSnapshot, gameID))
}

func (s *StateStore) SaveGameState(ctx context.Context, gameID string, state interface{}) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.gameStateKey(gameID), data, defaultTTL).Err()
}

func (s *StateStore) GetGameState(ctx context.Context, gameID string, state interface{}) error {
	data, err := s.client.Get(ctx, s.gameStateKey(gameID)).Result()
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(data), state)
}

func (s *StateStore) DeleteGameState(ctx context.Context, gameID string) error {
	return s.client.Del(ctx, s.gameStateKey(gameID)).Err()
}

func (s *StateStore) SaveGameStateWithTTL(ctx context.Context, gameID string, state interface{}, ttl time.Duration) error {
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, s.gameStateKey(gameID), data, ttl).Err()
}

func (s *StateStore) GameStateExists(ctx context.Context, gameID string) bool {
	return s.client.Exists(ctx, s.gameStateKey(gameID)).Val() > 0
}

func (s *StateStore) ExpireGameState(ctx context.Context, gameID string, ttl time.Duration) error {
	return s.client.Expire(ctx, s.gameStateKey(gameID), ttl).Err()
}

func (s *StateStore) SaveSnapshot(ctx context.Context, gameID string, snapshot *pb.GameSnapshot) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}
	data, err := proto.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("proto marshal snapshot failed: %w", err)
	}
	return s.client.Set(ctx, s.snapshotKey(gameID, snapshot.FrameNumber), data, defaultTTL).Err()
}

func (s *StateStore) SaveSnapshotWithTTL(ctx context.Context, gameID string, snapshot *pb.GameSnapshot, ttl time.Duration) error {
	if snapshot == nil {
		return fmt.Errorf("snapshot is nil")
	}
	data, err := proto.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("proto marshal snapshot failed: %w", err)
	}
	return s.client.Set(ctx, s.snapshotKey(gameID, snapshot.FrameNumber), data, ttl).Err()
}

func (s *StateStore) GetSnapshot(ctx context.Context, gameID string, frame uint64) (*pb.GameSnapshot, error) {
	data, err := s.client.Get(ctx, s.snapshotKey(gameID, frame)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("redis get snapshot failed: %w", err)
	}

	snap := &pb.GameSnapshot{}
	if err := proto.Unmarshal(data, snap); err != nil {
		return nil, fmt.Errorf("proto unmarshal snapshot failed: %w", err)
	}
	return snap, nil
}

func (s *StateStore) DeleteSnapshot(ctx context.Context, gameID string, frame uint64) error {
	return s.client.Del(ctx, s.snapshotKey(gameID, frame)).Err()
}

func (s *StateStore) GetAllSnapshots(ctx context.Context, gameID string) ([]*pb.GameSnapshot, error) {
	var keys []string
	var err error

	pattern := s.snapshotPattern(gameID)

	if s.useCluster {
		err = s.client.ForEachMaster(ctx, func(ctx context.Context, shard *redis.Client) error {
			iter := shard.Scan(ctx, 0, pattern, 0).Iterator()
			for iter.Next(ctx) {
				keys = append(keys, iter.Val())
			}
			return iter.Err()
		})
	} else {
		iter := s.client.Scan(ctx, 0, pattern, 0).Iterator()
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		err = iter.Err()
	}

	if err != nil {
		return nil, fmt.Errorf("scan snapshot keys failed: %w", err)
	}

	if len(keys) == 0 {
		return []*pb.GameSnapshot{}, nil
	}

	snapshots := make([]*pb.GameSnapshot, 0, len(keys))
	for _, key := range keys {
		data, err := s.client.Get(ctx, key).Bytes()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			return nil, fmt.Errorf("get snapshot data failed: %w", err)
		}

		snap := &pb.GameSnapshot{}
		if err := proto.Unmarshal(data, snap); err != nil {
			return nil, fmt.Errorf("proto unmarshal snapshot failed: %w", err)
		}
		snapshots = append(snapshots, snap)
	}

	return snapshots, nil
}

func (s *StateStore) DeleteAllSnapshots(ctx context.Context, gameID string) (int64, error) {
	var keys []string
	var err error

	pattern := s.snapshotPattern(gameID)

	if s.useCluster {
		err = s.client.ForEachMaster(ctx, func(ctx context.Context, shard *redis.Client) error {
			iter := shard.Scan(ctx, 0, pattern, 0).Iterator()
			for iter.Next(ctx) {
				keys = append(keys, iter.Val())
			}
			return iter.Err()
		})
	} else {
		iter := s.client.Scan(ctx, 0, pattern, 0).Iterator()
		for iter.Next(ctx) {
			keys = append(keys, iter.Val())
		}
		err = iter.Err()
	}

	if err != nil {
		return 0, fmt.Errorf("scan snapshot keys failed: %w", err)
	}

	if len(keys) == 0 {
		return 0, nil
	}

	return s.client.Del(ctx, keys...).Result()
}

func (s *StateStore) SnapshotExists(ctx context.Context, gameID string, frame uint64) bool {
	return s.client.Exists(ctx, s.snapshotKey(gameID, frame)).Val() > 0
}

func (s *StateStore) ExpireSnapshot(ctx context.Context, gameID string, frame uint64, ttl time.Duration) error {
	return s.client.Expire(ctx, s.snapshotKey(gameID, frame), ttl).Err()
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

func (s *StateStore) ActionCount(ctx context.Context, gameID string) int64 {
	return s.client.LLen(ctx, s.actionKey(gameID)).Val()
}

func (s *StateStore) ClearActions(ctx context.Context, gameID string) error {
	return s.client.Del(ctx, s.actionKey(gameID)).Err()
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

func (s *StateStore) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return s.client.Expire(ctx, key, expiration).Err()
}

func (s *StateStore) TTL(ctx context.Context, key string) time.Duration {
	return s.client.TTL(ctx, key).Val()
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

func (s *StateStore) HDel(ctx context.Context, key string, fields ...string) error {
	return s.client.HDel(ctx, key, fields...).Err()
}

func (s *StateStore) HExists(ctx context.Context, key, field string) bool {
	return s.client.HExists(ctx, key, field).Val()
}

func (s *StateStore) HLen(ctx context.Context, key string) int64 {
	return s.client.HLen(ctx, key).Val()
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

func (s *StateStore) ZRangeWithScores(ctx context.Context, key string, start, stop int64) ([]redis.Z, error) {
	return s.client.ZRangeWithScores(ctx, key, start, stop).Result()
}

func (s *StateStore) ZRank(ctx context.Context, key string, member interface{}) (int64, error) {
	return s.client.ZRank(ctx, key, member).Result()
}

func (s *StateStore) ZScore(ctx context.Context, key string, member interface{}) (float64, error) {
	return s.client.ZScore(ctx, key, member).Result()
}

func (s *StateStore) Incr(ctx context.Context, key string) int64 {
	return s.client.Incr(ctx, key).Val()
}

func (s *StateStore) IncrBy(ctx context.Context, key string, value int64) int64 {
	return s.client.IncrBy(ctx, key, value).Val()
}

func (s *StateStore) Decr(ctx context.Context, key string) int64 {
	return s.client.Decr(ctx, key).Val()
}

func (s *StateStore) DecrBy(ctx context.Context, key string, value int64) int64 {
	return s.client.DecrBy(ctx, key, value).Val()
}

func (s *StateStore) Close() error {
	return s.client.Close()
}

func (s *StateStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}
