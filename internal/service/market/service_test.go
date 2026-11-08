package market

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	entity "depeg.local/platform/internal/domain/market"
	"depeg.local/platform/internal/port"
)

// 数据库替身：记录查询次数，并返回固定数据。
type repositoryStub struct {
	// 本组测试只使用 ListAssets，其余仓储方法不使用。
	port.MarketRepository

	calls  int
	assets []entity.Asset
}

func (r *repositoryStub) ListAssets(
	ctx context.Context,
) ([]entity.Asset, error) {
	r.calls++

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.assets, nil
}

// 缓存替身：每个测试可以分别指定读取、写入的行为。
type cacheStub struct {
	get func(context.Context) ([]entity.Asset, bool, error)
	set func(context.Context, []entity.Asset, time.Duration) error

	setCalls int
}

func (c *cacheStub) GetAsset(
	ctx context.Context,
) ([]entity.Asset, bool, error) {
	if c.get != nil {
		return c.get(ctx)
	}
	// 默认模拟缓存未命中。
	return nil, false, nil
}

func (c *cacheStub) SetAsset(
	ctx context.Context,
	assets []entity.Asset,
	ttl time.Duration,
) error {
	c.setCalls++

	if c.set != nil {
		return c.set(ctx, assets, ttl)
	}
	return nil
}

func (c *cacheStub) InvalidateAsset(context.Context) error {
	return nil
}

// 日志替身：记录 Error 被调用的次数。
type loggerStub struct {
	errorCalls int
}

func (*loggerStub) Info(context.Context, string, ...any) {}

func (l *loggerStub) Error(context.Context, string, ...any) {
	l.errorCalls++
}

func TestListAssetsCacheFailure(t *testing.T) {
	cases := []struct {
		name    string
		phase   string
		timeout bool
	}{
		{"read_timeout", "read", true},
		{"write_error", "write", false},
		{"write_timeout", "write", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 原请求的预算较长，用来区分缓存自己的短时限。
			ctx, cancel := context.WithTimeout(
				context.Background(),
				2*time.Second,
			)
			defer cancel()

			repo := &repositoryStub{
				assets: []entity.Asset{
					{ID: "usdc", Symbol: "USDC"},
				},
			}
			cache := &cacheStub{}
			logger := &loggerStub{}

			fail := func(cacheCtx context.Context) error {
				if !tc.timeout {
					return errors.New("simulated Redis write failure")
				}

				// 确认缓存拿到的是较短的操作时限。
				// 200 ms 是为前述 100/50 ms 示例设置的检查上限。
				deadline, ok := cacheCtx.Deadline()
				if !ok {
					t.Fatal("缓存操作没有设置截止时间")
				}
				if time.Until(deadline) > 200*time.Millisecond {
					t.Fatal("缓存操作没有使用独立的短时限")
				}

				// 模拟缓存一直没有响应，直到上下文结束。
				<-cacheCtx.Done()
				return cacheCtx.Err()
			}

			if tc.phase == "read" {
				cache.get = func(
					cacheCtx context.Context,
				) ([]entity.Asset, bool, error) {
					return nil, false, fail(cacheCtx)
				}
			} else {
				cache.set = func(
					cacheCtx context.Context,
					_ []entity.Asset,
					_ time.Duration,
				) error {
					return fail(cacheCtx)
				}
			}

			service := New(repo, cache, logger)
			got, err := service.ListAssets(ctx)

			if err != nil {
				t.Fatalf("应返回数据库结果，实际错误：%v", err)
			}
			if !reflect.DeepEqual(got, repo.assets) {
				t.Fatalf("返回数据不正确：got=%v want=%v",
					got, repo.assets)
			}
			if repo.calls != 1 {
				t.Fatalf("数据库应查询一次，实际 %d 次", repo.calls)
			}
			if cache.setCalls != 1 {
				t.Fatalf("应尝试回写一次，实际 %d 次", cache.setCalls)
			}
			if logger.errorCalls != 1 {
				t.Fatalf("应记录一次缓存错误，实际 %d 次",
					logger.errorCalls)
			}
		})
	}
}

func TestListAssetsRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repo := &repositoryStub{}
	cache := &cacheStub{}
	logger := &loggerStub{}

	cache.get = func(
		cacheCtx context.Context,
	) ([]entity.Asset, bool, error) {
		// 确定请求已经进入缓存读取后，再取消原请求。
		cancel()

		<-cacheCtx.Done()
		return nil, false, cacheCtx.Err()
	}

	service := New(repo, cache, logger)
	_, err := service.ListAssets(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("应返回请求取消错误，实际：%v", err)
	}
	if repo.calls != 0 {
		t.Fatalf("请求取消后不应查询数据库，实际查询 %d 次",
			repo.calls)
	}
	if cache.setCalls != 0 {
		t.Fatalf("请求取消后不应回写缓存，实际回写 %d 次",
			cache.setCalls)
	}
}