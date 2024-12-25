// ui.go
package main

import (
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Constants used throughout the code
const (
	modelName   = "llama3.1"       // Example model name for Ollama
	httpTimeout = 30 * time.Second // Example timeout for HTTP requests
	appIconPath = "app.ico"        // Path to the application icon
)

// Global variables to hold the application state (could be in a struct).
var (
	myApp         fyne.App           // The main Fyne app instance
	myWindow      fyne.Window        // The main window
	chatData      binding.StringList // Binding for chat messages
	scroll        *container.Scroll  // Scroll container for chat history
	currentChatID int                // Tracks which chat is currently active
)

// initializeApp sets up the main Fyne application window and loads or creates the initial chat.
func initializeApp() {
	myApp = app.NewWithID("ollama.gui")
	myWindow = myApp.NewWindow("Ollama GUI")

	// Attempt to load an application icon
	appIcon := loadAppIcon(appIconPath)
	if appIcon != nil {
		myWindow.SetIcon(appIcon)
	}

	createMenuBar() // Create the main menu bar

	chatData = binding.NewStringList()

	ensureInitialChat()
	buildUI()

	myWindow.CenterOnScreen()
	myWindow.Resize(fyne.NewSize(800, 600))
	myWindow.ShowAndRun()
}

// ensureInitialChat checks if DB is empty; if so, creates a "Welcome Chat"
func ensureInitialChat() {
	var chatCount int
	err := db.QueryRow("SELECT COUNT(*) FROM chats").Scan(&chatCount)
	if err != nil {
		log.Printf("Failed to count chats: %v", err)
	}

	if chatCount == 0 {
		// Insert an initial "Welcome Chat"
		res, err := db.Exec("INSERT INTO chats (title, hash) VALUES (?, ?)", "Welcome Chat", "")
		if err != nil {
			log.Printf("Failed to create initial chat: %v", err)
			return
		}
		newID, _ := res.LastInsertId()
		currentChatID = int(newID)

		// Store a welcome message
		initialMessage := []string{"assistant: Welcome to the chat!"}
		chatData.Set(initialMessage)
		saveCurrentChat()
	} else {
		// Otherwise, load the first chat found in the DB
		var firstChatID int
		err := db.QueryRow("SELECT id FROM chats ORDER BY id LIMIT 1").Scan(&firstChatID)
		if err == nil {
			currentChatID = firstChatID
			loadChatHistory(firstChatID)
		}
	}
}

// buildUI constructs the main UI layout with a sidebar list and chat pane.
func buildUI() {
	chatsList, err := loadChatList()
	if err != nil {
		dialog.ShowError(err, myWindow)
		return
	}

	// Build the sidebar
	serverList := widget.NewList(
		func() int { return len(chatsList) + 1 }, // +1 for "New Chat"
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			hbox := container.NewBorder(nil, nil, nil, deleteButton, label)
			return hbox
		},
		func(id widget.ListItemID, o fyne.CanvasObject) {
			border := o.(*fyne.Container)
			var label *widget.Label
			var deleteBtn *widget.Button
			for _, obj := range border.Objects {
				switch c := obj.(type) {
				case *widget.Label:
					label = c
				case *widget.Button:
					deleteBtn = c
				}
			}
			if id == 0 {
				label.SetText("New Chat")
				deleteBtn.Hide()
				deleteBtn.OnTapped = nil
			} else {
				chat := chatsList[id-1]
				label.SetText(chat.title)
				deleteBtn.Show()
				deleteBtn.OnTapped = func() {
					if err := deleteChat(chat.id); err != nil {
						dialog.ShowError(err, myWindow)
						return
					}
					updateSidebar() // Refresh the sidebar after deletion
				}
			}
		},
	)
	serverList.OnSelected = func(id widget.ListItemID) {
		if id == 0 {
			handleNewChatClick()
		} else {
			chat := chatsList[id-1]
			handleSavedChatClick(chat.id)
		}
	}

	mainUI := makeMainUI(serverList)
	myWindow.SetContent(mainUI)
}

// createMenuBar creates the top menubar with various options (Theme toggle, About, etc.).
func createMenuBar() {
	themeToggle := fyne.NewMenuItem("Toggle Theme", func() {
		pref := myApp.Preferences()
		isDark := !pref.Bool("dark_mode")
		pref.SetBool("dark_mode", isDark)
		setTheme(isDark)
	})

	// Construct the main menu
	menu := fyne.NewMainMenu(
		fyne.NewMenu("Settings",
			fyne.NewMenuItem("Preferences", func() {
				dialog.ShowInformation("Preferences", "Settings menu under construction.", myWindow)
			}),
			fyne.NewMenuItem("About", func() {
				dialog.ShowInformation("About", "Ollama Chat App Version 1.0", myWindow)
			}),
		),
		fyne.NewMenu("Models",
			fyne.NewMenuItem("Models", func() {
				dialog.ShowInformation("View Models", "Feature to view and edit models will be added.", myWindow)
			}),
			fyne.NewMenuItem("Download Model", func() {
				dialog.ShowInformation("Download Model", "Feature to download models will be added.", myWindow)
			}),
		),
		fyne.NewMenu("Tools",
			fyne.NewMenuItem("Tools", func() {
				dialog.ShowInformation("Tools", "Tools coming soon.", myWindow)
			}),
			fyne.NewMenuItem("Create Tool", func() {
				dialog.ShowInformation("Create Tool", "Create Tool Feature coming soon.", myWindow)
			}),
			fyne.NewMenuItem("Built in Tools", func() {
				dialog.ShowInformation("Built in Tools", "Built in tools will be added.", myWindow)
			}),
			fyne.NewMenuItem("Export Chat", func() {
				dialog.ShowInformation("Export Chat", "Export functionality will be added.", myWindow)
			}),
		),
		fyne.NewMenu("Theme", themeToggle),
	)

	myWindow.SetMainMenu(menu)
}

// rebuildChatHistory clears and rebuilds the scrollable chat UI from the chatData binding.
func rebuildChatHistory() {
	chatContent := scroll.Content.(*fyne.Container)
	chatContent.Objects = nil

	items, _ := chatData.Get()
	for _, message := range items {
		role, content := parseRoleAndContent(message)
		isUser := (role == "user")

		if role == "user" || role == "assistant" {
			chatContent.Add(createChatBubble(content, isUser))
		}
	}
	scroll.ScrollToBottom()
}

// makeMainUI sets up the main horizontal split: left sidebar (chat list) and right panel (chat history + input).
func makeMainUI(serverList *widget.List) fyne.CanvasObject {
	chatHistory := createChatHistory()

	// Create the message input field, the upload button, and the send button
	messageInput, uploadButton, sendButton := createInputComponents()

	// Lay out the input components along the bottom
	inputContainer := container.NewBorder(nil, nil, uploadButton, sendButton, messageInput)
	messagePane := container.NewBorder(nil, inputContainer, nil, nil, chatHistory)

	// Split layout: left panel (serverList) and right panel (messagePane)
	mainContent := container.NewHSplit(serverList, messagePane)
	mainContent.SetOffset(0.2) // 20% for the sidebar, 80% for the chat content
	return mainContent
}

// setTheme switches between dark and light mode for the app.
func setTheme(isDark bool) {
	if isDark {
		myApp.Settings().SetTheme(theme.DarkTheme())
	} else {
		myApp.Settings().SetTheme(theme.LightTheme())
	}
}

// createChatHistory builds a scrollable container that displays all messages in chatData.
func createChatHistory() *fyne.Container {
	chatContent := container.NewVBox()
	scroll = container.NewVScroll(chatContent)
	scroll.SetMinSize(fyne.NewSize(300, 400))

	var displayedItems []string // local copy to detect changes

	// When chatData changes, update the UI
	chatData.AddListener(binding.NewDataListener(func() {
		newItems, _ := chatData.Get()

		// If we have more items than previously, append the new ones
		for i := len(displayedItems); i < len(newItems); i++ {
			role, content := parseRoleAndContent(newItems[i])
			isUser := (role == "user")
			bubble := createChatBubble(content, isUser)
			chatContent.Add(bubble)
		}

		// If any item was modified in place, refresh that bubble
		minLen := min(len(displayedItems), len(newItems))
		for i := 0; i < minLen; i++ {
			if newItems[i] != displayedItems[i] {
				role, content := parseRoleAndContent(newItems[i])
				isUser := (role == "user")
				chatContent.Objects[i] = createChatBubble(content, isUser)
			}
		}

		displayedItems = newItems

		// Scroll to bottom if new messages are appended
		if len(newItems) > 0 {
			scroll.ScrollToBottom()
		}
	}))
	scroll.ScrollToBottom()
	return container.New(layout.NewStackLayout(), scroll)
}

// parseRoleAndContent splits a message string into role ("user", "assistant", or "system") and content.
func parseRoleAndContent(line string) (string, string) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		// Default to "system" if no explicit role is found
		return "system", line
	}
	role := strings.TrimSpace(parts[0])
	content := strings.TrimSpace(parts[1])
	return role, content
}

// createChatBubble generates the UI for a single chat message bubble.
func createChatBubble(message string, isUser bool) *fyne.Container {
	label := widget.NewLabel(message)
	label.Wrapping = fyne.TextWrapWord

	// Use a stack container so we can have a background rectangle behind the text
	bubble := container.NewStack(
		canvasWithBackgroundAndCenteredInput(label, isUser),
	)

	// If user message, place it on the right; otherwise on the left
	if isUser {
		return container.NewHBox(layout.NewSpacer(), bubble)
	} else {
		return container.NewHBox(bubble, layout.NewSpacer())
	}
}

// canvasWithBackgroundAndCenteredInput creates a rounded rectangle behind the text.
func canvasWithBackgroundAndCenteredInput(content fyne.CanvasObject, isUser bool) fyne.CanvasObject {
	var bgColor color.Color
	if isUser {
		bgColor = color.Gray{Y: 128} // Gray color for the user background
	} else {
		bgColor = color.Transparent
	}

	roundedRect := canvas.NewRectangle(bgColor)
	roundedRect.SetMinSize(fyne.NewSize(600, content.MinSize().Height+20))
	roundedRect.StrokeColor = bgColor
	roundedRect.StrokeWidth = 0
	roundedRect.CornerRadius = 10

	return centeredContainer(container.NewStack(roundedRect, content))
}

// centeredContainer horizontally centers the content in a VBox with spacers.
func centeredContainer(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(
		layout.NewSpacer(),
		container.New(layout.NewCenterLayout(), container.NewStack(content)),
		layout.NewSpacer(),
	)
}

// createInputComponents returns the message input field, upload button, and send button.
func createInputComponents() (*widget.Entry, *widget.Button, *widget.Button) {
	messageInput := widget.NewEntry()
	messageInput.SetPlaceHolder("Type your message here...")

	sendMessage := func() {
		userMessage := strings.TrimSpace(messageInput.Text)
		if len(userMessage) > 500 {
			updateChatData("assistant: Error: Message too long. Please limit to 500 characters or use the file upload.")
			return
		}
		if userMessage != "" {
			updateChatData("user: " + userMessage)
			messageInput.SetText("")
			go handleUserMessage()
		}
	}

	messageInput.OnSubmitted = func(content string) {
		sendMessage()
	}

	uploadButton := widget.NewButton("+", func() {
		dialog.ShowInformation("File Upload", "Feature to upload files will be added.", myWindow)
	})

	sendButton := widget.NewButton("Send", sendMessage)

	return messageInput, uploadButton, sendButton
}

// updateChatData appends a new message to the chatData binding.
func updateChatData(message string) {
	items, _ := chatData.Get()
	chatData.Set(append(items, message))
	scroll.ScrollToBottom()
}

// updateSidebar refreshes the list of chats on the sidebar (left panel).
func updateSidebar() {
	buildUI()
}

// handleNewChatClick:
// 1. Saves the current chat (if any)
// 2. Creates a brand-new chat record in the DB
// 3. Sets chatData to have an initial welcome message from the assistant
// 4. Refreshes the sidebar
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

	chatData.Set([]string{"assistant: Welcome to your new chat!"})
	saveCurrentChat()

	// Refresh the UI
	updateSidebar()
}

// handleSavedChatClick loads an existing chat from the DB.
func handleSavedChatClick(chatID int) {
	saveCurrentChat()
	currentChatID = chatID
	loadChatHistory(chatID)
}

// min is a small helper to get the smaller of two int values.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// loadAppIcon attempts to load an icon file from the given path and returns a fyne.Resource.
func loadAppIcon(relativePath string) fyne.Resource {
	absPath, err := filepath.Abs(relativePath)
	if err != nil {
		fmt.Printf("Failed to resolve app icon path: %v\n", err)
		return nil
	}

	iconData, err := os.ReadFile(absPath)
	if err != nil {
		fmt.Printf("Failed to load app icon: %v\n", err)
		return nil
	}
	return fyne.NewStaticResource("app.ico", iconData)
}
