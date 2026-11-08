package redis

import (
	"context"
	"depeg.local/platform/internal/config"
	"depeg.local/platform/internal/port"
	"time"
	"fmt"

	"github.com/redis/go-redis/v9"
)


type Client struct {
	settings config.Redis
	logger   port.Logger
	rdb	  *redis.Client
}

func New(settings config.Redis, logger port.Logger) (port.RedisClient, error) {
	
	if settings.Timeout <= 0 {
		settings.Timeout = 5 * time.Second
	}

	rdb:= redis.NewClient(&redis.Options{
		Addr:     settings.Address,
		Password: settings.Password,
		DB:       settings.Database,
		DialTimeout:  settings.Timeout,
		ReadTimeout:  settings.Timeout,
		WriteTimeout: settings.Timeout,
		Username: settings.Username,
		ContextTimeoutEnabled: true,
	})
	return &Client{
		settings: settings, 
		logger: logger,
		rdb: rdb}, nil
}
func (c*Client) Ping(ctx context.Context) error {
	
	ctx,cancel:= context.WithTimeout(ctx, c.settings.Timeout)
	defer cancel()
	_, err := c.rdb.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}
	return nil
}
func (c*Client) Get(ctx context.Context,key string) ([]byte, error) {
	
	ctx,cancel := context.WithTimeout(ctx, c.settings.Timeout)
	defer cancel()
	val, err := c.rdb.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("redis key not found: %w", err)
	}else if err!=nil &&err!=redis.Nil{
		return nil, fmt.Errorf("redis get failed: %w", err)
	}
	return val,nil
}
func (c*Client) Set(ctx context.Context,key string,dst []byte,TTL time.Duration) error {
	
	ctx,cancel := context.WithTimeout(ctx, c.settings.Timeout)
	defer cancel()
	err := c.rdb.Set(ctx, key, dst, TTL).Err()
	if err != nil {
		return fmt.Errorf("redis set failed: %w", err)
	}
	return nil
}
func (c*Client) Delete(ctx context.Context,key string) error {
	
	ctx,cancel := context.WithTimeout(ctx, c.settings.Timeout)
	defer cancel()
	err := c.rdb.Del(ctx, key).Err()
	if err != nil {
		return fmt.Errorf("redis delete failed: %w", err)
	}
	return nil
}

func (c*Client) Eval(ctx context.Context,script string,key []string,args ...any) (any, error) {
	ctx,cancel := context.WithTimeout(ctx, c.settings.Timeout)
	defer cancel()
	
	result, err := c.rdb.Eval(ctx, script, key, args...).Result()
	if err != nil {
		return nil, fmt.Errorf("redis eval failed: %w", err)
	}
	return result, nil
}

func (c*Client) Close() error {
	
	
	return c.rdb.Close()
}
