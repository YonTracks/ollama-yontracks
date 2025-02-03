// ollama.go
package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ollama/ollama/api"
)

// Global variables for assistant response state
var (
	assistantIndex          = -1
	assistantMessageBuilder strings.Builder
)

// Mutex and timer for throttling UI updates
var (
	updateMutex sync.Mutex
	updateTimer *time.Timer
	updateDelay = 50 * time.Millisecond // Adjust as needed
)

// scheduleUIUpdate throttles UI updates to at most once per updateDelay.
func scheduleUIUpdate() {
	updateMutex.Lock()
	defer updateMutex.Unlock()

	if updateTimer != nil {
		return
	}

	updateTimer = time.AfterFunc(updateDelay, func() {
		rebuildChatHistory()
		scroll.ScrollToBottom()
		updateMutex.Lock()
		updateTimer = nil
		updateMutex.Unlock()
	})
}

// handleUserMessage streams a response from Ollama based on the conversation so far.
func handleUserMessage() {
	if err := streamFromOllama(); err != nil {
		updateChatData("assistant: Error: " + fmt.Sprintf("%v", err))
	}
	saveCurrentChat()
	// Optionally, update the UI further (for example, refresh a sidebar)
}

// streamFromOllama streams partial responses from the Ollama model and updates the chat UI in real–time.
func streamFromOllama() error {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("failed to create Ollama API client: %w", err)
	}

	items, _ := chatData.Get()
	var messages []api.Message
	for _, line := range items {
		role, content := parseRoleAndContent(line)
		if role != "user" && role != "assistant" && role != "system" {
			role = "system"
		}
		messages = append(messages, api.Message{
			Role:    role,
			Content: content,
		})
	}

	req := &api.ChatRequest{
		Model:    defaultModel,
		Messages: messages,
	}

	assistantMessageBuilder.Reset()
	assistantIndex = -1

	// Use a context with timeout to avoid hanging requests.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	respFunc := func(resp api.ChatResponse) error {
		assistantMessageBuilder.WriteString(resp.Message.Content)
		currentItems, _ := chatData.Get()

		if assistantIndex == -1 {
			newLine := "assistant: " + assistantMessageBuilder.String()
			currentItems = append(currentItems, newLine)
			chatData.Set(currentItems)
			assistantIndex = len(currentItems) - 1
		} else {
			currentItems, _ = chatData.Get()
			if assistantIndex < len(currentItems) {
				updatedLine := "assistant: " + assistantMessageBuilder.String()
				currentItems[assistantIndex] = updatedLine
				chatData.Set(currentItems)
			}
		}

		if resp.Done {
			assistantMessageBuilder.Reset()
			assistantIndex = -1
		}

		scheduleUIUpdate()
		return nil
	}

	if err := client.Chat(ctx, req, respFunc); err != nil {
		return fmt.Errorf("error in Ollama chat request: %w", err)
	}

	return nil
}
