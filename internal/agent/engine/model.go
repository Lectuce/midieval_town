package engine

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

type Model interface {
	Generate(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error)
}

type LLM struct {
	Client *anthropic.Client
}

func (m *LLM) Generate(ctx context.Context, params anthropic.MessageNewParams) (*anthropic.Message, error) {

	if m.Client == nil {
		return nil, fmt.Errorf("anthropic client is nil")
	}

	return m.Client.Messages.New(ctx, params)
}

type StreamingModel interface {
	GenerateStream(
		ctx context.Context,
		params anthropic.MessageNewParams,
		onText func(string) error,
	) (*anthropic.Message, error)
}

func (m *LLM) GenerateStream(
	ctx context.Context,
	params anthropic.MessageNewParams,
	onText func(string) error,
) (*anthropic.Message, error) {
	if m.Client == nil {
		return nil, fmt.Errorf("anthropic client is nil")
	}

	stream := m.Client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var message anthropic.Message
	completed := false

	for stream.Next() {
		event := stream.Current()
		// if event.Type == "content_block_delta" {
		// 	log.Printf(
		// 		"NPC stream delta_type=%s text_bytes=%d",
		// 		event.Delta.Type,
		// 		len(event.Delta.Text),
		// 	)
		// }
		// 将所有事件累积成完整消息，
		// 供后续工具执行和历史保存使用。
		err := message.Accumulate(event)
		if err != nil {
			return nil, fmt.Errorf("accumulate stream: %w", err)
		}

		// 只向前端发送普通文字
		if event.Type == "content_block_delta" &&
			event.Delta.Type == "text_delta" &&
			event.Delta.Text != "" &&
			onText != nil {
			err := onText(event.Delta.Text)
			if err != nil {
				return nil, fmt.Errorf("deliver stream text: %w", err)
			}
		}

		if event.Type == "message_stop" {
			completed = true
		}
	}

	err := stream.Err()
	if err != nil {
		return nil, fmt.Errorf("model stream: %w", err)
	}

	if !completed {
		return nil, fmt.Errorf("model stream ended before completion")
	}

	return &message, nil
}
