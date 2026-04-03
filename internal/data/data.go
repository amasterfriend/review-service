package data

import (
	"errors"
	"review-service/internal/conf"
	"review-service/internal/data/query"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(NewData, NewReviewRepo, NewESClient, NewRedisClient, NewDB)

// Data .
type Data struct {
	// TODO wrapped database client
	// db *gorm.DB
	query   *query.Query
	log     *log.Helper
	elastic *elasticsearch.TypedClient
	rdb     *redis.Client
	// hotTTL: 热缓存 TTL，优先命中，保证低延迟。
	hotTTL time.Duration
	// staleTTL: 兜底缓存 TTL，依赖异常时用于降级返回旧数据。
	staleTTL time.Duration
	// enableStaleFallback: 是否允许 ES 失败时回退到 stale cache。
	enableStaleFallback bool
}

// NewData .
func NewData(cfg *conf.Data, db *gorm.DB, esClient *elasticsearch.TypedClient, rdb *redis.Client, logger log.Logger) (*Data, func(), error) {
	cleanup := func() {
		log.Info("closing the data resources")
	}

	// 非常重要!为GEN生成的query代码设置数据库连接对象
	query.SetDefault(db)

	hotTTL := 10 * time.Second
	if cfg.GetCache() != nil && cfg.GetCache().GetHotTtl() != nil {
		hotTTL = cfg.GetCache().GetHotTtl().AsDuration()
	}
	staleTTL := 10 * time.Minute
	if cfg.GetCache() != nil && cfg.GetCache().GetStaleTtl() != nil {
		staleTTL = cfg.GetCache().GetStaleTtl().AsDuration()
	}
	enableStaleFallback := true
	if cfg.GetDegrade() != nil {
		enableStaleFallback = cfg.GetDegrade().GetEnableStaleFallback()
	}

	return &Data{
		query:               query.Q,
		elastic:             esClient,
		rdb:                 rdb,
		log:                 log.NewHelper(logger),
		hotTTL:              hotTTL,
		staleTTL:            staleTTL,
		enableStaleFallback: enableStaleFallback,
	}, cleanup, nil
}

func NewESClient(cfg *conf.ElasticSearch) (*elasticsearch.TypedClient, error) {
	es, err := elasticsearch.NewTypedClient(elasticsearch.Config{
		Addresses: cfg.Addresses,
	})
	if err != nil {
		return nil, err
	}
	return es, nil
}

func NewRedisClient(cfg *conf.Data) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr,
		WriteTimeout: cfg.Redis.WriteTimeout.AsDuration(),
		ReadTimeout:  cfg.Redis.ReadTimeout.AsDuration(),
	})
	return rdb, nil
}

func NewDB(cfg *conf.Data) (*gorm.DB, error) {
	switch strings.ToLower(cfg.Database.GetDriver()) {
	case "mysql":
		return gorm.Open(mysql.Open(cfg.Database.GetSource()))
	case "sqlite":
		return gorm.Open(sqlite.Open(cfg.Database.GetSource()))
	}
	return nil, errors.New("connect db fail unsupported db driver")
}
