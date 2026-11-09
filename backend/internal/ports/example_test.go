package ports_test

import (
	"context"
	"fmt"

	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
)

// The consumer depends only on ports.Agent. Replacing the fake needs no HTTP change.
func ExampleAgent() {
	var agent ports.Agent = testkit.Agent{Events: []ports.AgentEvent{{Kind: "answer.delta", Text: "Тестовый ответ"}}}
	_, err := agent.Run(testkit.Context(context.Background()), ports.AgentInput{ConversationID: "conversation_demo_1", AssistantMessageID: "msg_a1", Text: "Как запустить проект?"}, func(ctx context.Context, event ports.AgentEvent) error {
		fmt.Println(event.Text)
		return nil
	})
	fmt.Println(err)
	// Output:
	// Тестовый ответ
	// <nil>
}
