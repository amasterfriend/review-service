package service

import (
	"context"

	pb "review-service/api/review/v1"
	"review-service/internal/biz"
	"review-service/internal/data/model"

	"github.com/go-kratos/kratos/v2/transport"
)

// CreateReview C端创建评价
func (s *ReviewService) CreateReview(ctx context.Context, req *pb.CreateReviewRequest) (*pb.CreateReviewReply, error) {
	s.log.WithContext(ctx).Debugf("[service] CreateReview req=%v", req)
	// 参数转换
	// 调用biz层
	var anonymous int32
	if req.Anonymous {
		anonymous = 1
	}
	review, err := s.uc.CreateReview(ctx, &model.ReviewInfo{
		UserID:       req.UserID,
		OrderID:      req.OrderID,
		StoreID:      req.StoreID,
		Score:        req.Score,
		ServiceScore: req.ServiceScore,
		ExpressScore: req.ExpressScore,
		Content:      req.Content,
		PicInfo:      req.PicInfo,
		VideoInfo:    req.VideoInfo,
		Anonymous:    anonymous,
		Status:       10,
	})
	// 拼装返回结果
	if err != nil {
		return nil, err
	}
	return &pb.CreateReviewReply{ReviewID: review.ReviewID}, nil
}

// GetReview C端获取评价详情
func (s *ReviewService) GetReview(ctx context.Context, req *pb.GetReviewRequest) (*pb.GetReviewReply, error) {
	s.log.WithContext(ctx).Debugf("[service] GetReview req=%v", req)
	review, err := s.uc.GetReview(ctx, req.ReviewID)
	if err != nil {
		return nil, err
	}
	return &pb.GetReviewReply{Data: &pb.ReviewInfo{
		ReviewID:     review.ReviewID,
		UserID:       review.UserID,
		OrderID:      review.OrderID,
		Score:        review.Score,
		ServiceScore: review.ServiceScore,
		ExpressScore: review.ExpressScore,
		Content:      review.Content,
		PicInfo:      review.PicInfo,
		VideoInfo:    review.VideoInfo,
		Status:       review.Status,
	}}, nil
}

// // ListReviewByUserID C端根据用户ID分页查询评价列表
// func (s *ReviewService) ListReviewByUserID(ctx context.Context, req *pb.ListReviewByUserIDRequest) (*pb.ListReviewByUserIDReply, error) {
// 	fmt.Printf("[service] ListReviewByUserID req:%#v\n", req)
// 	dataList, err := s.uc.ListReviewByUserID(ctx, req.GetUserID(), int(req.GetPage()), int(req.GetSize()))
// 	if err != nil {
// 		return nil, err
// 	}
// 	list := make([]*pb.ReviewInfo, 0, len(dataList))
// 	for _, review := range dataList {
// 		list = append(list, &pb.ReviewInfo{
// 			ReviewID:     review.ReviewID,
// 			UserID:       review.UserID,
// 			OrderID:      review.OrderID,
// 			Score:        review.Score,
// 			ServiceScore: review.ServiceScore,
// 			ExpressScore: review.ExpressScore,
// 			Content:      review.Content,
// 			PicInfo:      review.PicInfo,
// 			VideoInfo:    review.VideoInfo,
// 			Status:       review.Status,
// 		})
// 	}
// 	return &pb.ListReviewByUserIDReply{List: list}, nil
// }

// ListReviewByStoreID C端根据商户ID分页查询评价列表
func (s *ReviewService) ListReviewByStoreID(ctx context.Context, req *pb.ListReviewByStoreIDRequest) (*pb.ListReviewByStoreIDReply, error) {
	s.log.WithContext(ctx).Debugf("[service] ListReviewByStoreID req=%v", req)
	result, err := s.uc.ListReviewByStoreID(ctx, req.GetStoreID(), req.GetPage(), req.GetSize())
	if err != nil {
		return nil, err
	}
	setDegradeHeader(ctx, result.Degraded, result.CacheLayer)
	// format
	list := make([]*pb.ReviewInfo, 0, len(result.List))
	for _, review := range result.List {
		list = append(list, &pb.ReviewInfo{
			ReviewID:     review.ReviewID,
			UserID:       review.UserID,
			OrderID:      review.OrderID,
			Score:        review.Score,
			ServiceScore: review.ServiceScore,
			ExpressScore: review.ExpressScore,
			Content:      review.Content,
			PicInfo:      review.PicInfo,
			VideoInfo:    review.VideoInfo,
			Status:       review.Status,
		})
	}
	s.log.WithContext(ctx).Debugf("[service] ListReviewByStoreID done store_id=%d degraded=%t cache_layer=%s size=%d", req.GetStoreID(), result.Degraded, result.CacheLayer, len(list))
	return &pb.ListReviewByStoreIDReply{List: list}, nil
}

// ReplyReview B端回复评价
func (s *ReviewService) ReplyReview(ctx context.Context, req *pb.ReplyReviewRequest) (*pb.ReplyReviewReply, error) {
	s.log.WithContext(ctx).Debugf("[service] ReplyReview req=%v", req)
	// 调用biz层
	reply, err := s.uc.CreateReply(ctx, &biz.ReplyParam{
		ReviewID:  req.GetReviewID(),
		StoreID:   req.GetStoreID(),
		Content:   req.GetContent(),
		PicInfo:   req.GetPicInfo(),
		VideoInfo: req.GetVideoInfo(),
	})
	if err != nil {
		return nil, err
	}
	return &pb.ReplyReviewReply{ReplyID: reply.ReplyID}, nil
}

// AppealReview B端申诉评价
func (s *ReviewService) AppealReview(ctx context.Context, req *pb.AppealReviewRequest) (*pb.AppealReviewReply, error) {
	s.log.WithContext(ctx).Debugf("[service] AppealReview req=%v", req)
	ret, err := s.uc.AppealReview(ctx, &biz.AppealParam{
		ReviewID:  req.GetReviewID(),
		StoreID:   req.GetStoreID(),
		Reason:    req.GetReason(),
		Content:   req.GetContent(),
		PicInfo:   req.GetPicInfo(),
		VideoInfo: req.GetVideoInfo(),
	})
	if err != nil {
		return nil, err
	}
	s.log.WithContext(ctx).Debugf("[service] AppealReview ret=%v", ret)
	return &pb.AppealReviewReply{AppealID: ret.AppealID}, nil
}

// AuditReview O端审核评价
func (s *ReviewService) AuditReview(ctx context.Context, req *pb.AuditReviewRequest) (*pb.AuditReviewReply, error) {
	s.log.WithContext(ctx).Debugf("[service] AuditReview req=%v", req)
	res, err := s.uc.AuditReview(ctx, &biz.AuditParam{
		ReviewID:  req.GetReviewID(),
		OpUser:    req.GetOpUser(),
		OpReason:  req.GetOpReason(),
		OpRemarks: req.GetOpRemarks(),
		Status:    req.GetStatus(),
	})
	if err != nil {
		return nil, err
	}
	return &pb.AuditReviewReply{
		ReviewID: req.ReviewID,
		Status:   res.Status,
	}, nil
}

// AuditAppeal O端审核商家申诉
func (s *ReviewService) AuditAppeal(ctx context.Context, req *pb.AuditAppealRequest) (*pb.AuditAppealReply, error) {
	s.log.WithContext(ctx).Debugf("[service] AuditAppeal req=%v", req)
	err := s.uc.AuditAppeal(ctx, &biz.AuditAppealParam{
		ReviewID: req.GetReviewID(),
		AppealID: req.GetAppealID(),
		OpUser:   req.GetOpUser(),
		Status:   req.GetStatus(),
	})
	if err != nil {
		return nil, err
	}
	return &pb.AuditAppealReply{}, nil
}

func setDegradeHeader(ctx context.Context, degraded bool, cacheLayer string) {
	tr, ok := transport.FromServerContext(ctx)
	if !ok {
		return
	}
	header := tr.ReplyHeader()
	if header == nil {
		return
	}
	if degraded {
		header.Set("X-Review-Degraded", "true")
	} else {
		header.Set("X-Review-Degraded", "false")
	}
	// 输出缓存层来源，便于客户端或网关快速识别是否命中降级/缓存层。
	if cacheLayer != "" {
		header.Set("X-Review-Cache-Layer", cacheLayer)
	}
}
