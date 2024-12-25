// ollama.go
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ollama/ollama/api"
)

// Assistant response tracking in global variables (for partial streaming updates).
var (
	assistantIndex          = -1
	assistantMessageBuilder strings.Builder
)

// handleUserMessage streams a response from Ollama (the LLM) based on the conversation so far.
func handleUserMessage() {
	if err := streamFromOllama(); err != nil {
		// Show an error in the chat
		updateChatData("assistant: Error: " + fmt.Sprintf("%v", err))
	}
	saveCurrentChat()
	// The UI (e.g., a call to updateSidebar()) can happen if desired
}

// streamFromOllama streams partial responses from the Ollama model and updates the chat UI in real-time.
func streamFromOllama() error {
	// 1) Create a new Ollama client from environment variables/config
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("failed to create Ollama API client: %w", err)
	}

	// 2) Gather the entire conversation from chatData
	items, _ := chatData.Get()
	var messages []api.Message
	for _, line := range items {
		role, content := parseRoleAndContent(line)
		// Normalize role to system/user/assistant
		if role != "user" && role != "assistant" && role != "system" {
			role = "system"
		}
		messages = append(messages, api.Message{
			Role:    role,
			Content: content,
		})
	}

	// 3) Build a ChatRequest with the entire conversation
	req := &api.ChatRequest{
		Model:    modelName,
		Messages: messages,
	}

	assistantMessageBuilder.Reset()
	assistantIndex = -1

	// 4) Define how to handle each partial response from Ollama
	respFunc := func(resp api.ChatResponse) error {
		assistantMessageBuilder.WriteString(resp.Message.Content)

		items, _ := chatData.Get()

		if assistantIndex == -1 {
			// First chunk
			newLine := "assistant: " + assistantMessageBuilder.String()
			chatData.Set(append(items, newLine))
			assistantIndex = len(items) // zero-based index for appended line
		} else {
			// Subsequent chunks
			items, _ := chatData.Get()
			if assistantIndex < len(items) {
				updatedLine := "assistant: " + assistantMessageBuilder.String()
				items[assistantIndex] = updatedLine
				chatData.Set(items)
				rebuildChatHistory()
				scroll.ScrollToBottom()
			}
		}

		// If streaming is done, you could handle final actions here:
		if resp.Done {
			assistantMessageBuilder.Reset()
			assistantIndex = -1
			rebuildChatHistory()
			scroll.ScrollToBottom()
		}
		return nil
	}

	// 5) Send the conversation to Ollama and stream responses
	if err := client.Chat(context.Background(), req, respFunc); err != nil {
		return fmt.Errorf("error in Ollama chat request: %w", err)
	}

	return nil
}
