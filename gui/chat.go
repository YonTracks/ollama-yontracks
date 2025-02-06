// chat.go
package main

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// createChatHistory builds a scrollable container that displays chatData.
func createChatHistory() *fyne.Container {
	chatContent := container.NewVBox()
	scroll = container.NewVScroll(chatContent)
	scroll.SetMinSize(fyne.NewSize(300, 400))

	var displayedItems []string

	chatData.AddListener(binding.NewDataListener(func() {
		newItems, _ := chatData.Get()
		for i := len(displayedItems); i < len(newItems); i++ {
			role, content := parseRoleAndContent(newItems[i])
			chatContent.Add(createChatBubble(content, role == "user"))
		}
		minLen := min(len(displayedItems), len(newItems))
		for i := 0; i < minLen; i++ {
			if newItems[i] != displayedItems[i] {
				role, content := parseRoleAndContent(newItems[i])
				chatContent.Objects[i] = createChatBubble(content, role == "user")
			}
		}
		displayedItems = newItems
		if len(newItems) > 0 {
			scroll.ScrollToBottom()
		}
	}))
	scroll.ScrollToBottom()
	return container.New(layout.NewStackLayout(), scroll)
}

// rebuildChatHistory rebuilds the scrollable chat UI from chatData.
func rebuildChatHistory() {
	chatContent := scroll.Content.(*fyne.Container)
	chatContent.Objects = nil

	items, _ := chatData.Get()
	for _, message := range items {
		role, content := parseRoleAndContent(message)
		if role == "user" || role == "assistant" {
			chatContent.Add(createChatBubble(content, role == "user"))
		}
	}
}

// updateChatData appends a new message to chatData.
func updateChatData(message string) {
	items, _ := chatData.Get()
	chatData.Set(append(items, message))
	scroll.ScrollToBottom()
}

// handleNewChatClick creates a new chat.
func handleNewChatClick() {
	saveCurrentChat()

	newTitle := fmt.Sprintf("Chat %d", getNextChatNumber())
	stmt, err := db.Prepare("INSERT INTO chats (title, hash) VALUES (?, ?)")
	if err != nil {
		dialog.ShowError(fmt.Errorf("Failed to prepare new chat statement: %w", err), myWindow)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(newTitle, "")
	if err != nil {
		dialog.ShowError(fmt.Errorf("Failed to create new chat: %w", err), myWindow)
		return
	}

	newID, _ := res.LastInsertId()
	currentChatID = int(newID)

	// Show the current model in the welcome message
	chatData.Set([]string{"assistant: Welcome to your new chat! Current model: " + currentModel})
	saveCurrentChat()

	// Refresh the sidebar
	makeSidebar()
}

// parseRoleAndContent splits a message string into role and content.
func parseRoleAndContent(line string) (string, string) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return "system", line
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

// createChatBubble generates the UI for a single chat message bubble.
func createChatBubble(message string, isUser bool) *fyne.Container {

	label := widget.NewLabel(message)
	label.Wrapping = fyne.TextWrapWord
	// Render the message using the markdown renderer to support code blocks.
	// Todo: Add support for copying text/code from code blocks and other markdown features.
	content := renderMarkdown(label.Text)

	bubble := container.NewStack(canvasWithBackgroundAndCenteredInput(content, isUser))
	if isUser {
		return container.NewHBox(layout.NewSpacer(), bubble)
	}
	return container.NewHBox(bubble, layout.NewSpacer())
}

// handleSavedChatClick loads an existing chat.
func handleSavedChatClick(chatID int) {
	saveCurrentChat()
	currentChatID = chatID
	loadChatHistory(chatID)
	makeSidebar()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
