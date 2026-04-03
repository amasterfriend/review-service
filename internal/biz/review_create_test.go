package biz

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/go-kratos/kratos/v2/log"

	"review-service/internal/data/model"
	"review-service/pkg/snowflake"
)

type createReviewRepo struct {
	existing []*model.ReviewInfo
	saved    *model.ReviewInfo
}

func (f *createReviewRepo) SaveReview(_ context.Context, review *model.ReviewInfo) (*model.ReviewInfo, error) {
	f.saved = review
	return review, nil
}

func (f *createReviewRepo) GetReviewByOrderID(context.Context, int64) ([]*model.ReviewInfo, error) {
	return f.existing, nil
}

func (f *createReviewRepo) GetReview(context.Context, int64) (*model.ReviewInfo, error) {
	return nil, errors.New("not implemented")
}

func (f *createReviewRepo) SaveReply(context.Context, *model.ReviewReplyInfo) (*model.ReviewReplyInfo, error) {
	return nil, errors.New("not implemented")
}

func (f *createReviewRepo) AppealReview(context.Context, *AppealParam) (*model.ReviewAppealInfo, error) {
	return nil, errors.New("not implemented")
}

func (f *createReviewRepo) AuditReview(context.Context, *AuditParam) (*model.ReviewInfo, error) {
	return nil, errors.New("not implemented")
}

func (f *createReviewRepo) AuditAppeal(context.Context, *AuditAppealParam) error {
	return errors.New("not implemented")
}

func (f *createReviewRepo) ListReviewByStoreID(context.Context, int64, int, int) (*ListReviewResult, error) {
	return nil, errors.New("not implemented")
}

type noopAIAuditor struct{}

func (noopAIAuditor) AuditReview(context.Context, *model.ReviewInfo) (*AIAuditResult, error) {
	return nil, errors.New("not implemented")
}

func TestCreateReviewDefaultPendingStatus(t *testing.T) {
	if err := snowflake.Init("2026-03-21", 1); err != nil {
		t.Fatalf("init snowflake failed: %v", err)
	}

	repo := &createReviewRepo{}
	uc := NewReviewUsecase(repo, noopAIAuditor{}, log.NewStdLogger(io.Discard))

	review := &model.ReviewInfo{
		OrderID: 1,
		Status:  0,
		Content: "评价内容测试",
	}
	_, err := uc.CreateReview(context.Background(), review)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if repo.saved == nil {
		t.Fatal("expected review to be saved")
	}
	if repo.saved.Status != 10 {
		t.Fatalf("want status=10, got=%d", repo.saved.Status)
	}
	if repo.saved.ReviewID == 0 {
		t.Fatal("expected generated review id")
	}
}
