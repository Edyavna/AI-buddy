// agent-demo replays a deterministic scenario without paid APIs or database persistence.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Edyavna/AI-buddy/backend/internal/agent"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"github.com/Edyavna/AI-buddy/backend/internal/testkit"
	"os"
)

func tool(name, args string) agent.Message {
	return agent.Message{Calls: []agent.Call{{ID: name, Type: "function", Function: agent.Function{Name: name, Arguments: args}}}}
}
func main() {
	fmt.Println("OFFLINE DEMO: scripted model, test adapters, no paid APIs, no database persistence")
	a := agent.Agent{
		Model: &agent.Script{Replies: []agent.Message{
			tool("get_my_tasks", `{}`), tool("search_knowledge", `{"query":"как поднять проект"}`), tool("read_document", `{"document_id":"doc_local_setup"}`),
			tool("get_access_status", `{"resource_id":"repo_demo_backend","permission":"read"}`),
			tool("prepare_access_request", `{"resource_id":"repo_demo_backend","permission":"read","reason":"Для первой учебной задачи нужен доступ к репозиторию"}`),
			{Content: `{"answer":"Для запуска нужен доступ к учебному репозиторию. Источник: doc_local_setup. Черновик подготовлен. Проверьте его и нажмите кнопку подтверждения в приложении.","outcome":"answered"}`},
		}},
		Knowledge: testkit.Knowledge{}, Tasks: testkit.Tasks{}, Access: &testkit.Access{}, Requests: testkit.Requests{},
		Drafts: testkit.Drafts(func(_ context.Context, in ports.DraftInput) (ports.Draft, error) {
			return ports.Draft{DraftInput: in, Version: 1, Revision: 1}, nil
		}),
	}
	input := ports.AgentInput{ConversationID: "offline-conversation", AssistantMessageID: "offline-assistant", Text: "Как поднять проект? Подготовь нужный доступ."}
	encoder := json.NewEncoder(os.Stdout)
	result, err := a.Run(testkit.Context(context.Background()), input, func(_ context.Context, event ports.AgentEvent) error { return encoder.Encode(event) })
	if err != nil {
		fmt.Fprintln(os.Stderr, "Offline scenario failed")
		os.Exit(1)
	}
	encoder.Encode(result)
}
