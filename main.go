package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/cloudwego/eino-ext/devops"
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
	if _, err := CreateSimpleChain(ctx); err != nil {
		log.Fatalf("Failed to create simple chain: %v", err)
	}

	if _, err := CreateAgentGraph(ctx); err != nil {
		log.Fatalf("Failed to create agent graph: %v", err)
	}

	// 创建两模型对话 graph
	twoModelRunner, err := CreateTwoModelChatGraph(ctx)
	if err != nil {
		log.Fatalf("Failed to create two model chat graph: %v", err)
	}

	// 运行两模型对话示例
	go func() {
		if err := RunTwoModelChat(ctx, twoModelRunner); err != nil {
			log.Printf("Two model chat error: %v", err)
		}
	}()

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
	log.Printf("✓ Graph 'two_model_chat' registered and running")
	log.Printf("\nAccess DevOps server via proxy: http://localhost:%d", ProxyPort)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", ProxyPort), proxyHandler); err != nil {
		log.Fatalf("Proxy server failed: %v", err)
	}
}
