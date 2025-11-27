package main

import (
	"context"
	"log"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

func CreateAgentGraph(ctx context.Context) (compose.Runnable[string, *schema.Message], error) {
	agentGraph := compose.NewGraph[string, *schema.Message]()

	config := &openai.ChatModelConfig{
		BaseURL: "https://aihubmix.com/v1",
		APIKey:  "sk-6kgtZQDkmZDQMfCo28C360320cEf45FaAf1577Ef08F4032b",
		Model:   "gpt-4o-mini",
	}

	chatModel, err := openai.NewChatModel(ctx, config)
	if err != nil {
		return nil, err
	}

	agentConfig := &adk.ChatModelAgentConfig{
		Name:        "assistant",
		Description: "A helpful assistant",
		Model:       chatModel,
	}

	agentRunnable, err := adk.NewChatModelAgent(ctx, agentConfig)
	if err != nil {
		return nil, err
	}

	var messageHistory []*schema.Message

	historyManagerLambda := compose.InvokableLambda(func(ctx context.Context, input string) ([]*schema.Message, error) {
		userMessage := &schema.Message{
			Role:    schema.User,
			Content: input,
		}

		messageHistory = append(messageHistory, userMessage)

		historyCopy := make([]*schema.Message, len(messageHistory))
		copy(historyCopy, messageHistory)

		log.Printf("History Manager: Added user message, history length: %d", len(messageHistory))
		return historyCopy, nil
	})

	agentLambda := compose.InvokableLambda(func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		agentInput := &adk.AgentInput{
			Messages: input,
		}

		iterator := agentRunnable.Run(ctx, agentInput)

		var finalMessage *schema.Message
		for {
			event, ok := iterator.Next()
			if !ok {
				break
			}

			msg, _, err := adk.GetMessage(event)
			if err == nil && msg != nil {
				finalMessage = msg
			}
		}

		if finalMessage == nil {
			finalMessage = &schema.Message{
				Role:    schema.Assistant,
				Content: "No response from agent",
			}
		}

		cleanMessage := &schema.Message{
			Role:    schema.Assistant,
			Content: finalMessage.Content,
		}
		messageHistory = append(messageHistory, cleanMessage)
		log.Printf("Agent: Generated response, history length: %d", len(messageHistory))

		return finalMessage, nil
	})

	agentGraph.AddLambdaNode("history_manager", historyManagerLambda)
	agentGraph.AddLambdaNode("agent_node", agentLambda)

	agentGraph.AddEdge(compose.START, "history_manager")
	agentGraph.AddEdge("history_manager", "agent_node")
	agentGraph.AddEdge("agent_node", compose.END)

	return agentGraph.Compile(ctx, compose.WithGraphName("agent_graph"))
}
