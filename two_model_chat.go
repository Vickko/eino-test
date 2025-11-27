package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"github.com/cloudwego/eino-ext/components/model/openai"
	callbacks2 "github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/cloudwego/eino/utils/callbacks"
)

type twoModelState struct {
	currentRound int
	msgs         []*schema.Message
}

func CreateTwoModelChatGraph(ctx context.Context) (compose.Runnable[[]*schema.Message, *schema.Message], error) {
	// 使用与 agent_graph 相同的配置
	config := &openai.ChatModelConfig{
		BaseURL:     "https://aihubmix.com/v1",
		APIKey:      "sk-6kgtZQDkmZDQMfCo28C360320cEf45FaAf1577Ef08F4032b",
		Model:       "gpt-4o-mini",
		Temperature: func() *float32 { v := float32(0.7); return &v }(),
	}

	llm, err := openai.NewChatModel(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("new chat model failed: %w", err)
	}

	g := compose.NewGraph[[]*schema.Message, *schema.Message](
		compose.WithGenLocalState(func(ctx context.Context) *twoModelState {
			return &twoModelState{}
		}),
	)

	// Writer node: 创作笑话或根据反馈修改
	writerPreHandler := func(ctx context.Context, input []*schema.Message, state *twoModelState) ([]*schema.Message, error) {
		state.currentRound++
		state.msgs = append(state.msgs, input...)
		systemMsg := schema.SystemMessage("you are a writer who writes jokes and revise it according to the critic's feedback. Prepend your joke with your name which is \"writer: \"")
		messages := append([]*schema.Message{systemMsg}, state.msgs...)
		log.Printf("Writer round %d: processing %d messages", state.currentRound, len(messages))
		return messages, nil
	}

	// Critic node: 对笑话给出反馈
	criticPreHandler := func(ctx context.Context, input []*schema.Message, state *twoModelState) ([]*schema.Message, error) {
		state.msgs = append(state.msgs, input...)
		systemMsg := schema.SystemMessage("you are a critic who ONLY gives feedback about jokes, emphasizing on funniness. Prepend your feedback with your name which is \"critic: \"")
		messages := append([]*schema.Message{systemMsg}, state.msgs...)
		log.Printf("Critic: processing %d messages", len(messages))
		return messages, nil
	}

	// 添加节点
	_ = g.AddChatModelNode("writer", llm,
		compose.WithStatePreHandler[[]*schema.Message, *twoModelState](writerPreHandler),
		compose.WithNodeName("writer"))

	_ = g.AddChatModelNode("critic", llm,
		compose.WithStatePreHandler[[]*schema.Message, *twoModelState](criticPreHandler),
		compose.WithNodeName("critic"))

	_ = g.AddLambdaNode("toList1", compose.ToList[*schema.Message]())
	_ = g.AddLambdaNode("toList2", compose.ToList[*schema.Message]())

	// 添加边和分支
	_ = g.AddEdge(compose.START, "writer")

	// Writer 的分支：根据轮次决定是继续还是结束
	_ = g.AddBranch("writer", compose.NewStreamGraphBranch(
		func(ctx context.Context, input *schema.StreamReader[*schema.Message]) (string, error) {
			// 读取 writer 的输出并打印
			var writerMessage *schema.Message
			for {
				msg, err := input.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return "", err
				}
				writerMessage = msg
			}

			if writerMessage != nil {
				log.Printf("Writer output: %s", writerMessage.Content)
			}

			next := "toList1"
			err := compose.ProcessState[*twoModelState](ctx, func(ctx context.Context, state *twoModelState) error {
				if state.currentRound >= 3 {
					next = compose.END
					log.Printf("Reached round 3, ending conversation")
				}
				return nil
			})
			if err != nil {
				return "", err
			}

			return next, nil
		},
		map[string]bool{compose.END: true, "toList1": true},
	))

	_ = g.AddEdge("toList1", "critic")
	_ = g.AddEdge("critic", "toList2")
	_ = g.AddEdge("toList2", "writer")

	return g.Compile(ctx, compose.WithGraphName("two_model_chat"))
}

// RunTwoModelChat 运行两个模型对话的示例
func RunTwoModelChat(ctx context.Context, runner compose.Runnable[[]*schema.Message, *schema.Message]) error {
	log.Println("=== Starting Two Model Chat (Writer vs Critic) ===")

	// 创建回调处理器来显示中间输出
	handler := callbacks.NewHandlerHelper().ChatModel(&callbacks.ModelCallbackHandler{
		OnEndWithStreamOutput: func(ctx context.Context, runInfo *callbacks2.RunInfo, input *schema.StreamReader[*model.CallbackOutput]) context.Context {
			defer input.Close()
			log.Println("\n=== Model Output ===")
			for {
				frame, err := input.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					log.Printf("Error reading stream: %v", err)
					break
				}
				fmt.Print(frame.Message.Content)
			}
			fmt.Println()
			return ctx
		},
	}).Handler()

	// 执行对话
	outStream, err := runner.Stream(ctx,
		[]*schema.Message{schema.UserMessage("write a funny line about robot, in 20 words.")},
		compose.WithCallbacks(handler))
	if err != nil {
		return fmt.Errorf("stream error: %w", err)
	}

	// 读取最终结果
	var finalMessage *schema.Message
	for {
		msg, err := outStream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("recv error: %w", err)
		}
		finalMessage = msg
	}

	if finalMessage != nil {
		log.Printf("\n=== Final Result ===\n%s\n", finalMessage.Content)
	}

	log.Println("=== Two Model Chat Completed ===")
	return nil
}
