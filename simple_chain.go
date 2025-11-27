package main

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

// CreateSimpleChain builds and compiles the demo chain so it can be registered with DevOps.
func CreateSimpleChain(ctx context.Context) (compose.Runnable[map[string]string, string], error) {
	simpleChain := compose.NewChain[map[string]string, string]()

	lambdaNode := compose.InvokableLambda(func(ctx context.Context, input map[string]string) (string, error) {
		name := input["name"]
		if name == "" {
			name = "World"
		}
		return fmt.Sprintf("Hello, %s!", name), nil
	})

	simpleChain.AppendLambda(lambdaNode)

	runnable, err := simpleChain.Compile(ctx, compose.WithGraphName("simple_chain"))
	if err != nil {
		return nil, fmt.Errorf("compile simple chain: %w", err)
	}

	return runnable, nil
}
