package biz

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"

	"review-service/internal/data/model"
)

type fakeReviewRepo struct {
	review      *model.ReviewInfo
	auditCalls  int
	auditParams []*AuditParam
}

func (f *fakeReviewRepo) SaveReview(context.Context, *model.ReviewInfo) (*model.ReviewInfo, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeReviewRepo) GetReviewByOrderID(context.Context, int64) ([]*model.ReviewInfo, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeReviewRepo) GetReview(context.Context, int64) (*model.ReviewInfo, error) {
	if f.review == nil {
		return nil, errors.New("review not found")
	}
	return f.review, nil
}

func (f *fakeReviewRepo) SaveReply(context.Context, *model.ReviewReplyInfo) (*model.ReviewReplyInfo, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeReviewRepo) AppealReview(context.Context, *AppealParam) (*model.ReviewAppealInfo, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeReviewRepo) AuditReview(_ context.Context, param *AuditParam) (*model.ReviewInfo, error) {
	f.auditCalls++
	copied := *param
	f.auditParams = append(f.auditParams, &copied)
	f.review.Status = param.Status
	f.review.OpReason = param.OpReason
	f.review.OpRemarks = param.OpRemarks
	f.review.OpUser = param.OpUser
	return f.review, nil
}

func (f *fakeReviewRepo) AuditAppeal(context.Context, *AuditAppealParam) error {
	return errors.New("not implemented")
}

func (f *fakeReviewRepo) ListReviewByStoreID(context.Context, int64, int, int) ([]*MyReviewInfo, error) {
	return nil, errors.New("not implemented")
}

type fakeAIAuditor struct {
	delay  time.Duration
	ret    *AIAuditResult
	err    error
	called int
}

func (f *fakeAIAuditor) AuditReview(ctx context.Context, _ *model.ReviewInfo) (*AIAuditResult, error) {
	f.called++
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.ret, nil
}

func newTestUsecase(repo ReviewRepo, ai ReviewAIAuditor) *ReviewUsecase {
	return NewReviewUsecase(repo, ai, log.NewStdLogger(io.Discard))
}

func TestAuditReviewFastAISuccess(t *testing.T) {
	repo := &fakeReviewRepo{review: &model.ReviewInfo{ReviewID: 1, Status: 10}}
	ai := &fakeAIAuditor{
		delay: 100 * time.Millisecond,
		ret:   &AIAuditResult{SuggestedStatus: 20, Reason: "ok", Risk: "low"},
	}
	uc := newTestUsecase(repo, ai)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := uc.AuditReview(ctx, &AuditParam{ReviewID: 1})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Status != 20 {
		t.Fatalf("want status=20, got=%d", res.Status)
	}
	if repo.auditCalls != 1 {
		t.Fatalf("want repo audit calls=1, got=%d", repo.auditCalls)
	}
	if repo.auditParams[0].OpUser != "AI" {
		t.Fatalf("want OpUser=AI, got=%s", repo.auditParams[0].OpUser)
	}
}

func TestAuditReviewTimeoutNoWrite(t *testing.T) {
	repo := &fakeReviewRepo{review: &model.ReviewInfo{ReviewID: 2, Status: 10}}
	ai := &fakeAIAuditor{
		delay: 1200 * time.Millisecond,
		ret:   &AIAuditResult{SuggestedStatus: 20, Reason: "ok", Risk: "low"},
	}
	uc := newTestUsecase(repo, ai)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, err := uc.AuditReview(ctx, &AuditParam{ReviewID: 2})
	if err == nil {
		t.Fatal("want timeout err, got nil")
	}
	if repo.auditCalls != 0 {
		t.Fatalf("want repo audit calls=0, got=%d", repo.auditCalls)
	}
	e := kerrors.FromError(err)
	if e.Code != 504 || e.Reason != "REQUEST_TIMEOUT" {
		t.Fatalf("want 504 REQUEST_TIMEOUT, got code=%d reason=%s", e.Code, e.Reason)
	}
}

func TestAuditReviewSlowAIButLongTimeoutSuccess(t *testing.T) {
	repo := &fakeReviewRepo{review: &model.ReviewInfo{ReviewID: 3, Status: 10}}
	ai := &fakeAIAuditor{
		delay: 1200 * time.Millisecond,
		ret:   &AIAuditResult{SuggestedStatus: 20, Reason: "ok", Risk: "low"},
	}
	uc := newTestUsecase(repo, ai)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	res, err := uc.AuditReview(ctx, &AuditParam{ReviewID: 3})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Status != 20 {
		t.Fatalf("want status=20, got=%d", res.Status)
	}
	if repo.auditCalls != 1 {
		t.Fatalf("want repo audit calls=1, got=%d", repo.auditCalls)
	}
}

func TestAuditReviewParentCanceled(t *testing.T) {
	repo := &fakeReviewRepo{review: &model.ReviewInfo{ReviewID: 4, Status: 10}}
	ai := &fakeAIAuditor{
		delay: 100 * time.Millisecond,
		ret:   &AIAuditResult{SuggestedStatus: 20, Reason: "ok", Risk: "low"},
	}
	uc := newTestUsecase(repo, ai)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := uc.AuditReview(ctx, &AuditParam{ReviewID: 4})
	if err == nil {
		t.Fatal("want canceled err, got nil")
	}
	if repo.auditCalls != 0 {
		t.Fatalf("want repo audit calls=0, got=%d", repo.auditCalls)
	}
	e := kerrors.FromError(err)
	if e.Code != 504 || e.Reason != "REQUEST_TIMEOUT" {
		t.Fatalf("want 504 REQUEST_TIMEOUT, got code=%d reason=%s", e.Code, e.Reason)
	}
}

func TestAuditReviewSecondCallBlockedByStatus(t *testing.T) {
	repo := &fakeReviewRepo{review: &model.ReviewInfo{ReviewID: 5, Status: 10}}
	ai := &fakeAIAuditor{ret: &AIAuditResult{SuggestedStatus: 20, Reason: "ok", Risk: "low"}}
	uc := newTestUsecase(repo, ai)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := uc.AuditReview(ctx, &AuditParam{ReviewID: 5})
	if err != nil {
		t.Fatalf("first call should succeed, got err=%v", err)
	}
	if repo.auditCalls != 1 {
		t.Fatalf("want repo audit calls=1 after first call, got=%d", repo.auditCalls)
	}

	_, err = uc.AuditReview(ctx, &AuditParam{ReviewID: 5})
	if err == nil {
		t.Fatal("second call should fail")
	}
	if !strings.Contains(err.Error(), "只有待审核状态的评论才能进行审核") {
		t.Fatalf("unexpected second-call err: %v", err)
	}
	if repo.auditCalls != 1 {
		t.Fatalf("want repo audit calls still=1, got=%d", repo.auditCalls)
	}
}
