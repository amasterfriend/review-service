package service

import (
	pb "review-service/api/review/v1"
	"review-service/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
)

// ProviderSet is service providers.
var ProviderSet = wire.NewSet(NewReviewService)

type ReviewService struct {
	pb.UnimplementedReviewServer
	uc  *biz.ReviewUsecase
	log *log.Helper
}

func NewReviewService(uc *biz.ReviewUsecase, logger log.Logger) *ReviewService {
	return &ReviewService{
		uc:  uc,
		log: log.NewHelper(logger),
	}
}
