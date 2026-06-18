package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/ristretto"
	"github.com/redis/go-redis/v9"
	pb "github.com/ecscard/game/internal/proto"
	"google.golang.org/protobuf/proto"
)

const (
	gameStateTTL = 1 * time.Hour
	snapshotTTL  = 5 * time.Minute

	keyPrefixGameState = "game:state:"
	keyPrefixSnapshot  = "game:snapshot:"
)

type CacheManager struct {
	localCache *ristretto.Cache
	redisClient redis.UniversalClient
	useCluster  bool

	hits   atomic.Int64
	misses atomic.Int64
}

func NewCacheManager(useCluster bool, redisAddrs []string, redisPassword string, redisDB int) (*CacheManager, error) {
	config := &ristretto.Config{
		NumCounters: 1e7,
		MaxCost:     1 << 30,
		BufferItems: 64,
	}

	localCache, err := ristretto.NewCache(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create ristretto cache: %w", err)
	}

	var redisClient redis.UniversalClient
	if useCluster {
		redisClient = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    redisAddrs,
			Password: redisPassword,
		})
	} else {
		var addr string
		if len(redisAddrs) > 0 {
			addr = redisAddrs[0]
		}
		redisClient = redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: redisPassword,
			DB:       redisDB,
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		localCache.Close()
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &CacheManager{
		localCache:  localCache,
		redisClient: redisClient,
		useCluster:  useCluster,
	}, nil
}

func (cm *CacheManager) Get(ctx context.Context, key string) ([]byte, error) {
	if val, found := cm.localCache.Get(key); found {
		cm.hits.Add(1)
		return val.([]byte), nil
	}

	cm.misses.Add(1)

	data, err := cm.redisClient.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("redis get failed: %w", err)
	}

	cm.localCache.SetWithTTL(key, data, int64(len(data)), gameStateTTL)

	return data, nil
}

func (cm *CacheManager) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	cm.localCache.SetWithTTL(key, value, int64(len(value)), ttl)

	pipe := cm.redisClient.Pipeline()
	pipe.Set(ctx, key, value, ttl)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis pipeline set failed: %w", err)
	}

	return nil
}

func (cm *CacheManager) Delete(ctx context.Context, key string) error {
	cm.localCache.Del(key)

	pipe := cm.redisClient.Pipeline()
	pipe.Del(ctx, key)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis pipeline delete failed: %w", err)
	}

	return nil
}

func (cm *CacheManager) Publish(ctx context.Context, channel string, msg interface{}) error {
	var data []byte
	var err error

	switch v := msg.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	case proto.Message:
		data, err = proto.Marshal(v)
		if err != nil {
			return fmt.Errorf("proto marshal failed: %w", err)
		}
	default:
		data, err = json.Marshal(v)
		if err != nil {
			return fmt.Errorf("json marshal failed: %w", err)
		}
	}

	if err := cm.redisClient.Publish(ctx, channel, data).Err(); err != nil {
		return fmt.Errorf("redis publish failed: %w", err)
	}

	return nil
}

func (cm *CacheManager) Subscribe(ctx context.Context, channels ...string) *redis.PubSub {
	return cm.redisClient.Subscribe(ctx, channels...)
}

func gameStateKey(gameID string) string {
	return keyPrefixGameState + gameID
}

func snapshotKey(gameID string, frame uint64) string {
	return fmt.Sprintf("%s%s:%d", keyPrefixSnapshot, gameID, frame)
}

func (cm *CacheManager) GetGameState(ctx context.Context, gameID string) (*pb.GameStatus, error) {
	key := gameStateKey(gameID)

	if val, found := cm.localCache.Get(key); found {
		cm.hits.Add(1)
		state := &pb.GameStatus{}
		if err := proto.Unmarshal(val.([]byte), state); err != nil {
			return nil, fmt.Errorf("proto unmarshal local cache failed: %w", err)
		}
		return state, nil
	}

	cm.misses.Add(1)

	data, err := cm.redisClient.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("redis get game state failed: %w", err)
	}

	state := &pb.GameStatus{}
	if err := proto.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("proto unmarshal redis failed: %w", err)
	}

	cm.localCache.SetWithTTL(key, data, int64(len(data)), gameStateTTL)

	return state, nil
}

func (cm *CacheManager) SaveGameState(ctx context.Context, gameID string, state *pb.GameStatus) error {
	key := gameStateKey(gameID)

	data, err := proto.Marshal(state)
	if err != nil {
		return fmt.Errorf("proto marshal game state failed: %w", err)
	}

	cm.localCache.SetWithTTL(key, data, int64(len(data)), gameStateTTL)

	pipe := cm.redisClient.Pipeline()
	pipe.Set(ctx, key, data, gameStateTTL)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis pipeline save game state failed: %w", err)
	}

	return nil
}

func (cm *CacheManager) GetSnapshot(ctx context.Context, gameID string, frame uint64) (*pb.GameSnapshot, error) {
	key := snapshotKey(gameID, frame)

	if val, found := cm.localCache.Get(key); found {
		cm.hits.Add(1)
		snap := &pb.GameSnapshot{}
		if err := proto.Unmarshal(val.([]byte), snap); err != nil {
			return nil, fmt.Errorf("proto unmarshal local cache failed: %w", err)
		}
		return snap, nil
	}

	cm.misses.Add(1)

	data, err := cm.redisClient.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, fmt.Errorf("redis get snapshot failed: %w", err)
	}

	snap := &pb.GameSnapshot{}
	if err := proto.Unmarshal(data, snap); err != nil {
		return nil, fmt.Errorf("proto unmarshal redis failed: %w", err)
	}

	cm.localCache.SetWithTTL(key, data, int64(len(data)), snapshotTTL)

	return snap, nil
}

func (cm *CacheManager) SaveSnapshot(ctx context.Context, gameID string, snap *pb.GameSnapshot) error {
	key := snapshotKey(gameID, snap.FrameNumber)

	data, err := proto.Marshal(snap)
	if err != nil {
		return fmt.Errorf("proto marshal snapshot failed: %w", err)
	}

	cm.localCache.SetWithTTL(key, data, int64(len(data)), snapshotTTL)

	pipe := cm.redisClient.Pipeline()
	pipe.Set(ctx, key, data, snapshotTTL)

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis pipeline save snapshot failed: %w", err)
	}

	return nil
}

func (cm *CacheManager) HitRate() float64 {
	hits := cm.hits.Load()
	misses := cm.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0.0
	}
	return float64(hits) / float64(total)
}

func (cm *CacheManager) LocalCacheSize() int64 {
	metrics := cm.localCache.Metrics
	if metrics == nil {
		return 0
	}
	return metrics.CostAdded() - metrics.CostEvicted()
}

func (cm *CacheManager) Close() error {
	cm.localCache.Close()
	return cm.redisClient.Close()
}

func (cm *CacheManager) ResetStats() {
	cm.hits.Store(0)
	cm.misses.Store(0)
}
