package server

import (
	"context"
	"fmt"
	"strings"
	"sync"

	v1 "review-service/api/review/v1"
	"review-service/internal/conf"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	"golang.org/x/time/rate"
)

type storeIDGetter interface {
	GetStoreID() int64
}

// keyedRateLimiter 为每个 key（这里是 method+storeID）维护独立桶，
// 目的是隔离热点商家，避免单个 store 的突发流量拖垮全局。
type keyedRateLimiter struct {
	qps      rate.Limit
	burst    int
	limiters sync.Map
}

func newKeyedRateLimiter(cfg *conf.Server_RateLimit_ListReviewByStore) *keyedRateLimiter {
	qps := cfg.GetQps()
	if qps <= 0 {
		qps = 50
	}
	burst := int(cfg.GetBurst())
	if burst <= 0 {
		burst = int(qps)
	}
	return &keyedRateLimiter{
		qps:   rate.Limit(qps),
		burst: burst,
	}
}

func (l *keyedRateLimiter) allow(key string) bool {
	// 首次出现的 key 动态创建 limiter，后续复用。
	actual, _ := l.limiters.LoadOrStore(key, rate.NewLimiter(l.qps, l.burst))
	limiter, ok := actual.(*rate.Limiter)
	if !ok {
		return false
	}
	return limiter.Allow()
}

// listReviewByStoreRateLimit 仅对 ListReviewByStoreID 生效的限流中间件。
// 限流 key 设计为 method + storeID，做到“同接口内热点隔离”。
func listReviewByStoreRateLimit(cfg *conf.Server) middleware.Middleware {
	limiter := newKeyedRateLimiter(cfg.GetRateLimit().GetListReviewByStore())
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			if !isListReviewByStoreOperation(ctx) {
				return next(ctx, req)
			}
			key := "unknown"
			if v, ok := req.(storeIDGetter); ok {
				key = fmt.Sprintf("ListReviewByStoreID:%d", v.GetStoreID())
			}
			// 超限统一返回业务错误码，便于网关和客户端识别治理信号。
			if !limiter.allow(key) {
				return nil, v1.ErrorRateLimited("请求过于频繁，请稍后重试")
			}
			return next(ctx, req)
		}
	}
}

// 通过 transport operation 精确匹配目标接口，避免误限流其他 RPC。
func isListReviewByStoreOperation(ctx context.Context) bool {
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return false
	}
	return strings.HasSuffix(tr.Operation(), "ListReviewByStoreID")
}
