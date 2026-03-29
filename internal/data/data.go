package data

import (
	"errors"
	"review-service/internal/conf"
	"review-service/internal/data/query"
	"strings"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(NewData, NewReviewRepo, NewESClient, NewDB)

// Data .
type Data struct {
	// TODO wrapped database client
	// db *gorm.DB
	query   *query.Query
	log     *log.Helper
	elastic *elasticsearch.TypedClient
}

// NewData .
func NewData(db *gorm.DB, esClient *elasticsearch.TypedClient, logger log.Logger) (*Data, func(), error) {
	cleanup := func() {
		log.Info("closing the data resources")
	}

	// 非常重要!为GEN生成的query代码设置数据库连接对象
	query.SetDefault(db)

	return &Data{query: query.Q, elastic: esClient, log: log.NewHelper(logger)}, cleanup, nil
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

func NewDB(cfg *conf.Data) (*gorm.DB, error) {
	switch strings.ToLower(cfg.Database.GetDriver()) {
	case "mysql":
		return gorm.Open(mysql.Open(cfg.Database.GetSource()))
	case "sqlite":
		return gorm.Open(sqlite.Open(cfg.Database.GetSource()))
	}
	return nil, errors.New("connect db fail unsupported db driver")
}
