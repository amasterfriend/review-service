package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"review-service/internal/biz"
	"review-service/internal/data/model"

	openaiModel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/schema"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(NewAIAuditor)

const (
	reviewAuditPrompt = `你是电商平台的评价审核助手。
你必须判断评价是否合规，并只输出 JSON，不要输出其他文本。

审核标准：
1. 若包含辱骂、歧视、色情、政治敏感、违法信息、明显广告引流、恶意人身攻击，判为不通过。
2. 普通差评、情绪表达、客观吐槽属于正常评价，不应误判为不通过。
3. 如果信息不足，默认通过。

输出 JSON 格式：
{"suggested_status":20,"reason":"...","risk":"low"}

字段约束：
- suggested_status 只能是 20(审核通过) 或 30(审核不通过)
- reason 是一句话原因
- risk 只能是 low / high`
	defaultAILocalTimeout = 3 * time.Second
)

type client struct {
	model *openaiModel.ChatModel
	log   *log.Helper
}

type aiReply struct {
	SuggestedStatus int32  `json:"suggested_status"`
	Reason          string `json:"reason"`
	Risk            string `json:"risk"`
}

func NewAIAuditor(logger log.Logger) (biz.ReviewAIAuditor, error) {
	apiKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if apiKey == "" {
		return nil, errors.New("OPENAI_API_KEY is required")
	}

	modelName := strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if modelName == "" {
		modelName = "gpt-4o-mini"
	}

	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))

	chatModel, err := openaiModel.NewChatModel(context.Background(), &openaiModel.ChatModelConfig{
		APIKey:  apiKey,
		Model:   modelName,
		BaseURL: baseURL,
		Timeout: 20 * time.Second,
	})
	if err != nil {
		return nil, err
	}

	return &client{
		model: chatModel,
		log:   log.NewHelper(logger),
	}, nil
}

func (c *client) AuditReview(ctx context.Context, review *model.ReviewInfo) (*biz.AIAuditResult, error) {
	if review == nil {
		return nil, errors.New("review is nil")
	}

	llmCtx, cancel := deriveTimeoutContext(ctx, defaultAILocalTimeout)
	defer cancel()

	msg, err := c.model.Generate(llmCtx, []*schema.Message{
		schema.SystemMessage(reviewAuditPrompt),
		schema.UserMessage(fmt.Sprintf("请审核以下评论内容：%s", review.Content)),
	})
	if err != nil {
		return nil, err
	}

	c.log.WithContext(ctx).Debugf("[ai] audit review_id=%d raw=%s", review.ReviewID, msg.Content)

	var reply aiReply
	if err := json.Unmarshal([]byte(msg.Content), &reply); err != nil {
		return nil, err
	}

	if reply.SuggestedStatus != 20 && reply.SuggestedStatus != 30 {
		return nil, fmt.Errorf("invalid suggested_status: %d", reply.SuggestedStatus)
	}
	if reply.Risk != "low" && reply.Risk != "high" {
		return nil, fmt.Errorf("invalid risk: %s", reply.Risk)
	}

	return &biz.AIAuditResult{
		SuggestedStatus: reply.SuggestedStatus,
		Reason:          strings.TrimSpace(reply.Reason),
		Risk:            reply.Risk,
	}, nil
}

func deriveTimeoutContext(parent context.Context, max time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		return context.WithTimeout(context.Background(), max)
	}
	if deadline, ok := parent.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return context.WithCancel(parent)
		}
		if remaining < max {
			max = remaining
		}
	}
	return context.WithTimeout(parent, max)
}
