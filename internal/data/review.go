package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"review-service/internal/biz"
	"review-service/internal/data/model"
	"review-service/internal/data/query"
	"review-service/pkg/snowflake"
	"strconv"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8/typedapi/types"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type reviewRepo struct {
	data *Data
	log  *log.Helper
}

// NewReviewRepo .
func NewReviewRepo(data *Data, logger log.Logger) biz.ReviewRepo {
	return &reviewRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

func (r *reviewRepo) SaveReview(ctx context.Context, review *model.ReviewInfo) (*model.ReviewInfo, error) {
	err := r.data.query.ReviewInfo.
		WithContext(ctx).
		Save(review)
	return review, err
}

// GetReviewByOrderID 根据订单ID查询评价
func (r *reviewRepo) GetReviewByOrderID(ctx context.Context, orderID int64) ([]*model.ReviewInfo, error) {
	return r.data.query.ReviewInfo.
		WithContext(ctx).
		Where(r.data.query.ReviewInfo.OrderID.Eq(orderID)).
		Find()
}

// GetReview 根据评价ID查询评价
func (r *reviewRepo) GetReview(ctx context.Context, reviewID int64) (*model.ReviewInfo, error) {
	return r.data.query.ReviewInfo.
		WithContext(ctx).
		Where(r.data.query.ReviewInfo.ReviewID.Eq(reviewID)).
		First()
}

var g singleflight.Group

func (r *reviewRepo) getData1(ctx context.Context, storeID int64, offset, limit int) ([]*biz.MyReviewInfo, error) {
	// 去ES里面查询评价
	resp, err := r.data.elastic.Search().Index("review").
		From(offset).
		Size(limit).
		Query(&types.Query{
			Bool: &types.BoolQuery{
				Filter: []types.Query{
					{
						Term: map[string]types.TermQuery{
							"store_id": {
								Value: storeID,
							},
						},
					},
				},
			},
		}).
		Do(ctx)
	if err != nil {
		return nil, err
	}
	fmt.Printf("ListReviewByStoreID, es resp:%v\n", resp.Hits.Total.Value)
	// 反序列化数据
	// resp.Hits.Hits[0].Source_ ---> json.RawMessage ---> model.ReviewInfo
	list := make([]*biz.MyReviewInfo, 0, resp.Hits.Total.Value)
	for _, hit := range resp.Hits.Hits {
		tmp := biz.MyReviewInfo{}
		if err := json.Unmarshal(hit.Source_, &tmp); err != nil {
			r.log.WithContext(ctx).Errorf("ListReviewByStoreID unmarshal review fail, err:%v", err)
			continue
		}
		list = append(list, &tmp)
	}
	return list, nil
}

// 升级版带缓存版本的查询函数
func (r *reviewRepo) getData2(ctx context.Context, storeID int64, offset, limit int) ([]*biz.MyReviewInfo, error) {
	// 取数据
	// 1. 先从缓存中查询数据
	// 2. 如果缓存中没有，再查询ES
	// 3. 通过singleflight合并短时间内大量并发请求
	key := fmt.Sprintf("review:%d:%d:%d", storeID, offset, limit)
	val, err := r.getDataBySingleflight(ctx, key)
	if err != nil {
		return nil, err
	}
	hm := new(types.HitsMetadata)
	if err := json.Unmarshal(val, hm); err != nil {
		return nil, err
	}
	// 反序列化数据
	list := make([]*biz.MyReviewInfo, 0, hm.Total.Value)
	for _, hit := range hm.Hits {
		tmp := &biz.MyReviewInfo{}
		if err := json.Unmarshal(hit.Source_, tmp); err != nil {
			r.log.Errorf("json.Unmarshal fail, err:%v", err)
			continue
		}
		list = append(list, tmp)
	}
	return list, nil
}

func (r *reviewRepo) getDataBySingleflight(ctx context.Context, key string) ([]byte, error) {
	v, err, shared := g.Do(key, func() (interface{}, error) {
		// 先从缓存中查询数据
		val, err := r.getDataFromCache(ctx, key)
		if err == nil {
			return val, nil
		}
		// 如果缓存中没有这个key，再查询ES
		if errors.Is(err, redis.Nil) {
			r.log.Debugf("cache miss for key:%s", key)
			data, err := r.getDataFromES(ctx, key)
			if err == nil {
				// 返回结果，设置缓存
				return data, r.setCache(ctx, key, data)
			}
			return nil, err
		}
		// 其他错误，直接返回错误，不继续向下传导压力
		return nil, err
	})
	r.log.Debugf("getDataBySingleflight, key:%s, val:%s, err:%v, shared:%v", key, string(v.([]byte)), err, shared)
	return v.([]byte), err
}

// getDataFromCache 读缓存
func (r *reviewRepo) getDataFromCache(ctx context.Context, key string) ([]byte, error) {
	val, err := r.data.rdb.Get(ctx, key).Bytes()
	r.log.Debugf("getDataFromCache, key:%s, val:%s, err:%v", key, string(val), err)
	if err != nil {
		return nil, err
	}
	return val, nil
}

// setCache 写缓存
func (r *reviewRepo) setCache(ctx context.Context, key string, val []byte) error {
	err := r.data.rdb.Set(ctx, key, val, time.Second*10).Err()
	r.log.Debugf("setCache, key:%s, val:%s, err:%v", key, string(val), err)
	return err
}

// getDataFromES 读ES
func (r *reviewRepo) getDataFromES(ctx context.Context, key string) ([]byte, error) {
	values := strings.Split(key, ":")
	if len(values) < 4 {
		return nil, errors.New("invalid key")
	}
	index, storeID, offsetStr, limitStr := values[0], values[1], values[2], values[3]
	offset, _ := strconv.Atoi(offsetStr)
	limit, _ := strconv.Atoi(limitStr)
	resp, err := r.data.elastic.Search().
		Index(index).
		From(offset).
		Size(limit).
		Query(&types.Query{
			Bool: &types.BoolQuery{
				Filter: []types.Query{
					{
						Term: map[string]types.TermQuery{
							"store_id": {
								Value: storeID,
							},
						},
					},
				},
			},
		}).
		Do(ctx)
	if err != nil {
		return nil, err
	}
	return json.Marshal(resp.Hits)
}

// ListReviewByStoreID 根据商家ID查询评价列表
func (r *reviewRepo) ListReviewByStoreID(ctx context.Context, storeID int64, offset, limit int) ([]*biz.MyReviewInfo, error) {
	// return r.getData1(ctx, storeID, offset, limit) // 直接查ES
	return r.getData2(ctx, storeID, offset, limit) // 增加缓存和singleflight的版本
}

// SaveReply 保存商家回复
func (r *reviewRepo) SaveReply(ctx context.Context, reply *model.ReviewReplyInfo) (*model.ReviewReplyInfo, error) {
	// 更新数据库中的数据（评价回复表和评价表要同时更新，涉及到事务操作）
	// 事务操作
	err := r.data.query.Transaction(func(tx *query.Query) error {
		// 回复表插入一条数据
		if err := tx.ReviewReplyInfo.
			WithContext(ctx).
			Save(reply); err != nil {
			r.log.WithContext(ctx).Errorf("SaveReply create reply fail, err:%v", err)
			return err
		}
		// 评价表更新hasReply字段
		if _, err := tx.ReviewInfo.
			WithContext(ctx).
			Where(tx.ReviewInfo.ReviewID.Eq(reply.ReviewID)).
			Update(tx.ReviewInfo.HasReply, 1); err != nil {
			r.log.WithContext(ctx).Errorf("SaveReply update review fail, err:%v", err)
			return err
		}
		return nil
	})
	return reply, err
}

// AppealReview 申诉评价（商家对用户评价进行申诉）
func (r *reviewRepo) AppealReview(ctx context.Context, param *biz.AppealParam) (*model.ReviewAppealInfo, error) {
	// 先查询有没有申诉
	ret, err := r.data.query.ReviewAppealInfo.
		WithContext(ctx).
		Where(
			query.ReviewAppealInfo.ReviewID.Eq(param.ReviewID),
			query.ReviewAppealInfo.StoreID.Eq(param.StoreID),
		).First()
	r.log.Debugf("AppealReview query, ret:%v err:%v", ret, err)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		// 其他查询错误
		return nil, err
	}
	if err == nil && ret.Status > 10 {
		return nil, errors.New("该评价已有审核过的申诉记录")
	}
	// 查询不到审核过的申诉记录
	// 1. 有申诉记录但是处于待审核状态，需要更新
	// if ret != nil{
	// 	// update
	// }else{
	// 	// insert
	// }
	// 2. 没有申诉记录，需要创建
	appeal := &model.ReviewAppealInfo{
		ReviewID:  param.ReviewID,
		StoreID:   param.StoreID,
		Status:    10,
		Reason:    param.Reason,
		Content:   param.Content,
		PicInfo:   param.PicInfo,
		VideoInfo: param.VideoInfo,
	}
	if ret != nil {
		appeal.AppealID = ret.AppealID
	} else {
		appeal.AppealID = snowflake.GenID()
	}
	err = r.data.query.ReviewAppealInfo.
		WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "review_id"}, // ON DUPLICATE KEY
			},
			DoUpdates: clause.Assignments(map[string]interface{}{ // UPDATE
				"status":     appeal.Status,
				"content":    appeal.Content,
				"reason":     appeal.Reason,
				"pic_info":   appeal.PicInfo,
				"video_info": appeal.VideoInfo,
			}),
		}).
		Create(appeal) // INSERT
	r.log.Debugf("AppealReview, err:%v", err)
	return appeal, err
}

// AuditReview 审核评价（运营对用户的评价进行审核）
func (r *reviewRepo) AuditReview(ctx context.Context, param *biz.AuditParam) (*model.ReviewInfo, error) {
	res, err := r.data.query.ReviewInfo.
		WithContext(ctx).
		Where(r.data.query.ReviewInfo.ReviewID.Eq(param.ReviewID)).
		Updates(map[string]interface{}{
			"status":     param.Status,
			"op_user":    param.OpUser,
			"op_reason":  param.OpReason,
			"op_remarks": param.OpRemarks,
		})
	if err != nil {
		return nil, err
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("update failed, no rows affected")
	}
	return r.GetReview(ctx, param.ReviewID) // 审核完后查询一次
}

// AuditAppeal 审核申诉（运营对商家的申诉进行审核，审核通过会隐藏该评价）
func (r *reviewRepo) AuditAppeal(ctx context.Context, param *biz.AuditAppealParam) error {
	err := r.data.query.Transaction(func(tx *query.Query) error {
		// 申诉表
		if _, err := tx.ReviewAppealInfo.
			WithContext(ctx).
			Where(r.data.query.ReviewAppealInfo.AppealID.Eq(param.AppealID)).
			Updates(map[string]interface{}{
				"status":  param.Status,
				"op_user": param.OpUser,
			}); err != nil {
			return err
		}
		// 评价表
		if param.Status == 20 { // 申诉通过则需要隐藏评价
			if _, err := tx.ReviewInfo.WithContext(ctx).
				Where(tx.ReviewInfo.ReviewID.Eq(param.ReviewID)).
				Update(tx.ReviewInfo.Status, 40); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}
