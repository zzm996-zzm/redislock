package redislock

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

var (
	ErrLockNotHold         = errors.New("未持有锁")
	ErrFailedToPreemptLock = errors.New("加锁失败")

	//go:embed script/lua/unlock.lua
	luaUnlock string
	//go:embed script/lua/refresh.lua
	luaRefresh string
	//go:embed script/lua/lock.lua
	luaLock string
)

type Client struct {
	client redis.Cmdable
	g      singleflight.Group
	valuer func() string
}

type Lock struct {
	client     redis.Cmdable
	key        string
	val        string
	expiration time.Duration
	unlock     chan struct{}
}

func NewClient(client redis.Cmdable) *Client {
	return &Client{
		client: client,
		valuer: func() string {
			return uuid.New().String()
		},
	}
}

func newLock(client redis.Cmdable, key, val string, expiration time.Duration) *Lock {
	return &Lock{
		client:     client,
		key:        key,
		val:        val,
		expiration: expiration,
		unlock:     make(chan struct{}, 1),
	}
}

func (c *Client) SingleflightLock(ctx context.Context, key string, expiration time.Duration, retry RetryStrategy, timeout time.Duration) (*Lock, error) {
	for {
		flag := false
		result := c.g.DoChan(key, func() (interface{}, error) {
			flag = true
			return c.Lock(ctx, key, expiration, retry, timeout)
		})
		select {
		case res := <-result:
			if flag {
				c.g.Forget(key)
				if res.Err != nil {
					return nil, res.Err
				}
				return res.Val.(*Lock), nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// 流程先加锁，如果加锁失败则根据错误类型进行分析，如果是超时错误可以进行重试，但是有最大重试次数
// 使用timer 进行睡眠操作，interval 时间到达之后进行下一次循环，进行加锁操作

func (c *Client) Lock(ctx context.Context, key string,
	expiration time.Duration, retry RetryStrategy, timeout time.Duration) (*Lock, error) {
	val := c.valuer()
	var timer *time.Timer
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()

	for {
		lctx, cancel := context.WithTimeout(ctx, timeout)
		res, err := c.client.Eval(lctx, luaLock, []string{key}, val, expiration.Seconds()).Result()
		cancel()
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			// 非超时错误，那么基本上代表遇到了一些不可挽回的场景，所以没太大必要继续尝试了
			// 比如说 Redis server 崩了，或者 EOF 了
			return nil, err
		}
		if res == "OK" {
			return newLock(c.client, key, val, expiration), nil
		}
		interval, ok := retry.Next()
		if !ok {
			if err != nil {
				err = fmt.Errorf("最后一次重试错误: %w", err)
			} else {
				err = fmt.Errorf("锁被人持有: %w", ErrFailedToPreemptLock)
			}
			return nil, fmt.Errorf("rlock: 重试机会耗尽，%w", err)
		}
		if timer == nil {
			timer = time.NewTimer(interval)
		} else {
			timer.Reset(interval)
		}
		select {
		case <-timer.C:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// TryLock 尝试加锁,但不一定真的能拿到锁
func (c *Client) TryLock(ctx context.Context, key string, expiration time.Duration) (*Lock, error) {
	val := c.valuer()
	res, err := c.client.SetNX(ctx, key, val, expiration).Result()

	if err != nil {
		return nil, err
	}
	if !res {
		return nil, ErrFailedToPreemptLock
	}

	return newLock(c.client, key, val, expiration), nil

}

func (l *Lock) UnLock(ctx context.Context) error {

	// 调用unlock方法，默认需要解锁，则不再自动续约
	defer func() {
		l.unlock <- struct{}{}
		close(l.unlock)
	}()

	res, err := l.client.Eval(ctx, luaUnlock, []string{l.key}, l.val).Int64()

	if errors.Is(err, redis.Nil) {
		return ErrLockNotHold
	}

	if err != nil {
		return err
	}
	// 要判断 res 是不是 1
	if res == 0 {
		// 这把锁不是你的，或者这个 key 不存在
		return ErrLockNotHold
	}
	return nil
}

func (l *Lock) AutoRefresh(interval time.Duration, timeout time.Duration) error {
	ticker := time.NewTicker(interval)
	ch := make(chan struct{}, 1)

	defer ticker.Stop()
	defer close(ch)

	for {
		select {
		case <-ch:

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := l.Refresh(ctx)
			cancel()

			if errors.Is(err, context.DeadlineExceeded) {
				ch <- struct{}{}
				continue
			}
			if err != nil {
				return err
			}
		case <-ticker.C:

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			err := l.Refresh(ctx)
			cancel()

			if errors.Is(err, context.DeadlineExceeded) {
				ch <- struct{}{}
				continue
			}

			if err != nil {
				return err
			}
		case <-l.unlock:
			return nil
		}
	}
}

func (l *Lock) Refresh(ctx context.Context) error {
	res, err := l.client.Eval(ctx, luaRefresh, []string{l.key}, l.val, l.expiration).Int64()
	if errors.Is(err, redis.Nil) {
		return ErrLockNotHold
	}

	if err != nil {
		return err
	}
	// 要判断 res 是不是 1
	if res == 0 {
		// 这把锁不是你的，或者这个 key 不存在
		return ErrLockNotHold
	}
	return nil
}
