package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino-ext/devops"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	DefaultDevOpsPort = 52538 // eino devops 默认端口
	ProxyPort         = 52538
	DevOpsServerPort  = 52539 // devops 实际运行端口（默认端口+1）
)

func main() {
	ctx := context.Background()

	// 初始化 DevOps server，启动在默认端口+1 (52539)
	err := devops.Init(ctx, devops.WithDevServerPort(fmt.Sprintf("%d", DevOpsServerPort)))
	if err != nil {
		log.Fatalf("Failed to initialize DevOps server: %v", err)
	}

	// 创建一个简单的 chain 作为演示
	simpleChain := compose.NewChain[map[string]string, string]()

	// 创建一个 Lambda 节点
	lambdaNode := compose.InvokableLambda(func(ctx context.Context, input map[string]string) (string, error) {
		name := input["name"]
		if name == "" {
			name = "World"
		}
		return fmt.Sprintf("Hello, %s!", name), nil
	})

	simpleChain.AppendLambda(lambdaNode)

	// 编译 chain（会自动注册到 DevOps server）
	_, err = simpleChain.Compile(ctx, compose.WithGraphName("simple_chain"))
	if err != nil {
		log.Fatalf("Failed to compile chain: %v", err)
	}

	// 创建包含 agent 的图
	agentGraph := compose.NewGraph[string, *schema.Message]()

	// 配置 OpenAI chat model
	config := &openai.ChatModelConfig{
		BaseURL: "https://aihubmix.com/v1",
		APIKey:  "sk-6kgtZQDkmZDQMfCo28C360320cEf45FaAf1577Ef08F4032b",
		Model:   "gpt-4o-mini",
	}

	// 创建 chat model
	chatModel, err := openai.NewChatModel(ctx, config)
	if err != nil {
		log.Fatalf("Failed to create chat model: %v", err)
	}

	// 创建 agent 配置
	agentConfig := &adk.ChatModelAgentConfig{
		Name:        "assistant",
		Description: "A helpful assistant",
		Model:       chatModel,
	}

	// 创建 agent
	agentRunnable, err := adk.NewChatModelAgent(ctx, agentConfig)
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}

	// 历史记录存储（在闭包外部维护）
	var messageHistory []*schema.Message

	// 节点1: 历史记录管理器 - 接收 string，输出消息列表
	historyManagerLambda := compose.InvokableLambda(func(ctx context.Context, input string) ([]*schema.Message, error) {
		// 创建新的用户消息
		userMessage := &schema.Message{
			Role:    schema.User,
			Content: input,
		}

		// 添加到历史记录
		messageHistory = append(messageHistory, userMessage)

		// 返回完整的历史记录（副本）
		historyCopy := make([]*schema.Message, len(messageHistory))
		copy(historyCopy, messageHistory)

		log.Printf("History Manager: Added user message, history length: %d", len(messageHistory))
		return historyCopy, nil
	})

	// 节点2: Agent 节点 - 接收消息列表，输出单个消息
	agentLambda := compose.InvokableLambda(func(ctx context.Context, input []*schema.Message) (*schema.Message, error) {
		// 将消息列表传递给 agent
		agentInput := &adk.AgentInput{
			Messages: input,
		}

		// 调用 agent
		iterator := agentRunnable.Run(ctx, agentInput)

		// 获取最终的消息输出
		var finalMessage *schema.Message
		for {
			event, ok := iterator.Next()
			if !ok {
				break
			}

			// 尝试从事件中提取消息
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

		// 将 agent 的响应添加到历史记录（只保留 role 和 content）
		cleanMessage := &schema.Message{
			Role:    schema.Assistant,
			Content: finalMessage.Content,
		}
		messageHistory = append(messageHistory, cleanMessage)
		log.Printf("Agent: Generated response, history length: %d", len(messageHistory))

		// 返回完整的 agent 响应（包含所有字段）
		return finalMessage, nil
	})

	// 添加节点到图中
	agentGraph.AddLambdaNode("history_manager", historyManagerLambda)
	agentGraph.AddLambdaNode("agent_node", agentLambda)

	// 添加边：START -> history_manager -> agent_node -> END
	agentGraph.AddEdge(compose.START, "history_manager")
	agentGraph.AddEdge("history_manager", "agent_node")
	agentGraph.AddEdge("agent_node", compose.END)

	// 编译图
	_, err = agentGraph.Compile(ctx, compose.WithGraphName("agent_graph"))
	if err != nil {
		log.Fatalf("Failed to compile agent graph: %v", err)
	}

	// 在默认端口 (52538) 启动转发代理层
	target, _ := url.Parse(fmt.Sprintf("http://localhost:%d", DevOpsServerPort))
	proxy := httputil.NewSingleHostReverseProxy(target)

	// 修改响应，添加 CORS 头
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("Access-Control-Allow-Origin", "*")
		resp.Header.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		resp.Header.Set("Access-Control-Allow-Headers", "*")
		resp.Header.Set("Access-Control-Expose-Headers", "*")
		return nil
	}

	proxyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 处理 OPTIONS 预检请求
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			w.Header().Set("Access-Control-Expose-Headers", "*")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// 转发请求到 DevOps server
		proxy.ServeHTTP(w, r)
	})

	log.Printf("✓ DevOps server started on port %d", DevOpsServerPort)
	log.Printf("✓ Proxy server starting on port %d (default port)", ProxyPort)
	log.Printf("✓ CORS enabled for all origins")
	log.Printf("✓ Chain 'simple_chain' registered")
	log.Printf("✓ Graph 'agent_graph' registered")
	log.Printf("\nAccess DevOps server via proxy: http://localhost:%d", ProxyPort)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", ProxyPort), proxyHandler); err != nil {
		log.Fatalf("Proxy server failed: %v", err)
	}
}
