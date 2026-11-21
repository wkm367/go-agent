package history

// cache.go 定义历史会话缓存接口并实现 Redis 版本，
// 负责 JSON 读写、TTL 管理和带令牌校验的分布式锁。
import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Cache 抽象历史会话使用的键值缓存和分布式锁能力。
type Cache interface {
	Get(ctx context.Context, key string, destination any) (bool, error)
	Set(ctx context.Context, key string, value any, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	AcquireLock(ctx context.Context, key string, ttl time.Duration) (string, bool, error)
	ReleaseLock(ctx context.Context, key, token string) error
	Ping(ctx context.Context) error
}

// RedisCache 是基于 Redis 的 Cache 实现。
type RedisCache struct {
	client *redis.Client
}

// releaseLockScript 仅在锁值仍等于持有者令牌时删除锁，
// 防止一个请求误释放另一个请求重新获取的锁。
var releaseLockScript = redis.NewScript(`
if redis.call("get", KEYS[1]) == ARGV[1] then
    return redis.call("del", KEYS[1])
end
return 0
`)

// NewRedisCache 创建 Redis 缓存客户端。
func NewRedisCache(address, password string, db int) *RedisCache {
	return &RedisCache{
		client: redis.NewClient(&redis.Options{
			Addr:     address,
			Password: password,
			DB:       db,
		}),
	}
}

// Get 读取并反序列化缓存值；键不存在时返回 false。
func (c *RedisCache) Get(ctx context.Context, key string, destination any) (bool, error) {
	value, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(value, destination); err != nil {
		return false, err
	}
	return true, nil
}

// Set 序列化 value 并以指定 TTL 写入缓存。
func (c *RedisCache) Set(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, key, data, ttl).Err()
}

// Delete 删除一个或多个缓存键。
func (c *RedisCache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return c.client.Del(ctx, keys...).Err()
}

// AcquireLock 使用 SETNX 获取带过期时间的排他锁，并返回锁持有者令牌。
func (c *RedisCache) AcquireLock(
	ctx context.Context,
	key string,
	ttl time.Duration,
) (string, bool, error) {
	token := uuid.NewString()
	acquired, err := c.client.SetNX(ctx, key, token, ttl).Result()
	return token, acquired, err
}

// ReleaseLock 仅释放由 token 持有的锁。
func (c *RedisCache) ReleaseLock(ctx context.Context, key, token string) error {
	return releaseLockScript.Run(ctx, c.client, []string{key}, token).Err()
}

// Ping 检查 Redis 是否可用。
func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// Close 关闭 Redis 客户端并释放连接资源。
func (c *RedisCache) Close() error {
	return c.client.Close()
}
