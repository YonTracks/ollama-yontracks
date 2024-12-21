package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	// SQLite driver for database interactions
	_ "github.com/mattn/go-sqlite3"

	// Ollama API client
	"github.com/ollama/ollama/api"

	// Fyne imports for UI
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
	modelName   = "llama3.1"        // Example model name for Ollama
	httpTimeout = 30 * time.Second  // Example timeout for HTTP requests
	appIconPath = "app.ico"         // Path to the application icon
	dbFile      = "ollama_chats.db" // SQLite database file name
)

// Global variables to hold the application state
var (
	myApp         fyne.App    // The main Fyne app instance
	myWindow      fyne.Window // The main window
	chatData      binding.StringList
	scroll        *container.Scroll
	currentChatID int        // Tracks which chat is currently active
	db            *sql.DB    // Global DB connection
	mu            sync.Mutex // Mutex to prevent concurrent DB writes
)

func main() {
	// Initialize the database tables and connect to the database
	initializeDB()
	defer db.Close()

	// Initialize and run the GUI application
	initializeApp()
}

// initializeDB sets up the SQLite database (opens the connection and creates tables if needed).
func initializeDB() {
	var err error
	db, err = sql.Open("sqlite3", dbFile) // dbFile = ollama_chats.db
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}

	// Create the "chats" table if it doesn't exist
	execSQL(`
	CREATE TABLE IF NOT EXISTS chats (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT NOT NULL,
		hash TEXT NOT NULL
	);
	`)

	// Create the "messages" table if it doesn't exist
	execSQL(`
	CREATE TABLE IF NOT EXISTS messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id INTEGER NOT NULL,
		role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
		content TEXT NOT NULL,
		FOREIGN KEY(chat_id) REFERENCES chats(id) ON DELETE CASCADE
	);
	`)
}

// execSQL is a helper function to run a SQL statement (used for setup).
func execSQL(query string) {
	_, err := db.Exec(query)
	if err != nil {
		log.Fatalf("Failed to execute SQL: %v", err)
	}
}

// saveCurrentChat performs several actions to persist the current in-memory chat data to the database:
// 1. Checks if the current chat is new or existing (by checking a hash).
// 2. Updates the chat record in the DB if needed.
// 3. Deletes old messages for the current chat (to replace them).
// 4. Inserts new messages from the memory binding (chatData).
func saveCurrentChat() {
	mu.Lock()
	defer mu.Unlock()

	if currentChatID < 0 {
		// Means there's no valid "current" chat
		return
	}

	// Get all messages (strings) for the current chat
	currentChat := getCurrentChat()
	if len(currentChat) == 0 {
		// Nothing to save if there are no messages
		return
	}

	// Hash the content to track if the chat changed
	hash := hashChat(currentChat)

	// See if we already have a stored hash for this chat
	stmt, err := db.Prepare("SELECT hash FROM chats WHERE id = ?")
	if err != nil {
		log.Printf("Failed to prepare statement: %v", err)
		return
	}
	defer stmt.Close()

	var existingHash string
	err = stmt.QueryRow(currentChatID).Scan(&existingHash)
	if err == sql.ErrNoRows {
		// If there is no chat record, create a new one
		title := fmt.Sprintf("Chat %d", getNextChatNumber())
		insertChat(title, hash)
	} else if err != nil {
		log.Printf("Error checking chat: %v", err)
	} else if existingHash != hash {
		// If the hashes differ, update it
		updateChatHash(hash)
	}

	// Clear out old messages and insert fresh ones
	deleteOldMessages(currentChatID)
	insertMessages(currentChat)
	updateSidebar()
}

// insertChat creates a new record in the "chats" table, setting currentChatID to that new ID.
func insertChat(title string, hash string) {
	stmt, err := db.Prepare("INSERT INTO chats (title, hash) VALUES (?, ?)")
	if err != nil {
		log.Printf("Failed to prepare insert chat statement: %v", err)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(title, hash)
	if err != nil {
		log.Printf("Failed to insert new chat: %v", err)
		return
	}
	newID, _ := res.LastInsertId()
	currentChatID = int(newID)
}

// updateChatHash updates the stored hash for the current chat record.
func updateChatHash(hash string) {
	stmt, err := db.Prepare("UPDATE chats SET hash = ? WHERE id = ?")
	if err != nil {
		log.Printf("Failed to prepare update chat hash statement: %v", err)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(hash, currentChatID)
	if err != nil {
		log.Printf("Failed to update chat hash: %v", err)
	}
}

// deleteOldMessages removes all messages belonging to a particular chat ID.
func deleteOldMessages(chatID int) {
	stmt, err := db.Prepare("DELETE FROM messages WHERE chat_id = ?")
	if err != nil {
		log.Printf("Failed to prepare delete messages statement: %v", err)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(chatID)
	if err != nil {
		log.Printf("Failed to delete old messages: %v", err)
	}
	updateSidebar()
}

// insertMessages writes a slice of message strings to the database for the current chat.
func insertMessages(messages []string) {
	stmt, err := db.Prepare("INSERT INTO messages (chat_id, role, content) VALUES (?, ?, ?)")
	if err != nil {
		log.Printf("Failed to prepare insert messages statement: %v", err)
		return
	}
	defer stmt.Close()

	for _, line := range messages {
		role, content := parseRoleAndContent(line)
		if !isValidRole(role) {
			log.Printf("Invalid role detected: %s", role)
			continue
		}
		_, err := stmt.Exec(currentChatID, role, content)
		if err != nil {
			log.Printf("Failed to insert message: %v", err)
		}
	}
}

// isValidRole ensures the role is either "user" or "assistant".
func isValidRole(role string) bool {
	return role == "user" || role == "assistant"
}

// hashChat creates a SHA-256 hash of the entire chat content.
func hashChat(chat []string) string {
	hash := sha256.New()
	for _, line := range chat {
		hash.Write([]byte(line))
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// getCurrentChat retrieves the messages from the chatData binding as a slice of strings.
func getCurrentChat() []string {
	items, _ := chatData.Get()
	return items
}

// loadChatHistory pulls all messages for a given chat ID from the DB and loads them into chatData.
func loadChatHistory(chatID int) {
	mu.Lock()
	defer mu.Unlock()

	stmt, err := db.Prepare("SELECT role, content FROM messages WHERE chat_id = ?")
	if err != nil {
		log.Printf("Failed to prepare statement: %v", err)
		return
	}
	defer stmt.Close()

	rows, err := stmt.Query(chatID)
	if err != nil {
		log.Printf("Failed to load chat messages: %v", err)
		return
	}
	defer rows.Close()

	var messages []string
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			log.Printf("Error scanning message: %v", err)
			continue
		}
		// Combine them as "role: content"
		messages = append(messages, role+": "+content)
	}
	chatData.Set(messages)
}

// getNextChatNumber gets the number of existing chats, so we can name new chats accordingly (e.g., Chat 1, Chat 2...).
func getNextChatNumber() int {
	var count int
	stmt, err := db.Prepare("SELECT COUNT(*) FROM chats")
	if err != nil {
		log.Printf("Failed to prepare count chats statement: %v", err)
		return 1
	}
	defer stmt.Close()

	err = stmt.QueryRow().Scan(&count)
	if err != nil {
		log.Printf("Failed to count chats: %v", err)
		return 1
	}
	return count + 1
}

// updateSidebar refreshes the list of chats on the sidebar (left panel).
func updateSidebar() {
	chatsList, err := loadChatList()
	if err != nil {
		log.Printf("Failed to load chat list: %v", err)
		return
	}

	// Create a widget.List to display all chats plus a "New Chat" button at the top
	serverList := widget.NewList(
		func() int { return len(chatsList) + 1 },
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

			// Extract the label and delete button from the container
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
					deleteChat(chat.id)
				}
			}
		},
	)

	// When user clicks on an item, if "New Chat" is selected, create a new chat;
	// otherwise load the saved chat.
	serverList.OnSelected = func(id widget.ListItemID) {
		if id == 0 {
			handleNewChatClick()
		} else {
			chat := chatsList[id-1]
			handleSavedChatClick(chat.id)
		}
	}

	mainContent := makeMainUI(serverList)
	myWindow.SetContent(mainContent)
}

// chatRecord holds a single row from the "chats" table.
type chatRecord struct {
	id    int
	title string
	hash  string
}

// loadChatList retrieves all chats from the database (for sidebar display).
func loadChatList() ([]chatRecord, error) {
	rows, err := db.Query("SELECT id, title, hash FROM chats ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []chatRecord
	for rows.Next() {
		var r chatRecord
		if err := rows.Scan(&r.id, &r.title, &r.hash); err != nil {
			log.Printf("Error scanning chat list: %v", err)
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

// handleNewChatClick does the following:
// 1. Saves the current chat (if any)
// 2. Creates a brand-new chat record in the DB
// 3. Sets chatData to have an initial welcome message from the assistant
// 4. Refreshes the sidebar
func handleNewChatClick() {
	saveCurrentChat()

	newTitle := fmt.Sprintf("Chat %d", getNextChatNumber())
	stmt, err := db.Prepare("INSERT INTO chats (title, hash) VALUES (?, ?)")
	if err != nil {
		log.Printf("Failed to prepare new chat statement: %v", err)
		return
	}
	defer stmt.Close()

	res, err := stmt.Exec(newTitle, "")
	if err != nil {
		log.Printf("Failed to create new chat: %v", err)
		return
	}

	newID, _ := res.LastInsertId()
	currentChatID = int(newID)

	// Set an initial message in the new chat
	initialMessage := []string{"assistant: Welcome to your new chat!"}
	chatData.Set(initialMessage)

	// Save immediately so it persists
	saveCurrentChat()
	updateSidebar()
}

// handleSavedChatClick loads an existing chat from the DB.
func handleSavedChatClick(chatID int) {
	saveCurrentChat()
	currentChatID = chatID
	loadChatHistory(chatID)
}

// deleteChat removes the chat record and messages from the database, and resets the current chat if needed.
func deleteChat(chatID int) {
	stmt, err := db.Prepare("DELETE FROM chats WHERE id = ?")
	if err != nil {
		log.Printf("Failed to prepare delete chat statement: %v", err)
		return
	}
	defer stmt.Close()

	_, err = stmt.Exec(chatID)
	if err != nil {
		log.Printf("Failed to delete chat: %v", err)
	}

	// If the deleted chat was the current chat, pick another if available, else clear everything
	if chatID == currentChatID {
		chatsList, _ := loadChatList()
		if len(chatsList) > 0 {
			currentChatID = chatsList[0].id
			loadChatHistory(currentChatID)
		} else {
			currentChatID = -1
			chatData.Set([]string{})
		}
	}

	updateSidebar()
}

// makeMainUI sets up the main horizontal split: left sidebar (chat list) and right panel (chat history + input).
func makeMainUI(serverList *widget.List) fyne.CanvasObject {
	// The scrollable chat area
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

// parseRoleAndContent splits a message string into role ("user" or "assistant" or "system") and content.
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

// initializeApp sets up the main Fyne application window and loads or creates the initial chat.
func initializeApp() {
	myApp = app.NewWithID("ollama.gui")
	myWindow = myApp.NewWindow("Ollama GUI")

	// Attempt to load an application icon (optional)
	appIcon := loadAppIcon(appIconPath)
	if appIcon != nil {
		myWindow.SetIcon(appIcon)
	}

	createMenuBar() // Create the main menu bar

	chatData = binding.NewStringList()

	// If the database has no chats, create one by default
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

	// Build the sidebar list again here
	chatsList, _ := loadChatList()
	serverList := widget.NewList(
		func() int { return len(chatsList) + 1 },
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
					deleteChat(chat.id)
				}
			}
		},
	)

	// Handle click in the sidebar
	serverList.OnSelected = func(id widget.ListItemID) {
		if id == 0 {
			handleNewChatClick()
		} else {
			chatsList, _ := loadChatList()
			handleSavedChatClick(chatsList[id-1].id)
		}
	}

	mainUI := makeMainUI(serverList)

	myWindow.SetContent(mainUI)
	myWindow.CenterOnScreen()
	myWindow.Resize(fyne.NewSize(800, 600))
	myWindow.ShowAndRun()
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

	// Create a rectangle with corner radius for a "bubble" effect
	roundedRect := canvas.NewRectangle(bgColor)
	roundedRect.SetMinSize(fyne.NewSize(600, content.MinSize().Height+20))
	roundedRect.StrokeColor = bgColor
	roundedRect.StrokeWidth = 0
	roundedRect.CornerRadius = 10 // Adjust as needed

	// Stack the background rectangle behind the text
	return centeredContainer(container.NewStack(roundedRect, content))
}

// centeredContainer horizontally centers the content.
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

	// The function to execute when "Send" is clicked or the Enter key is pressed
	sendMessage := func() {
		userMessage := strings.TrimSpace(messageInput.Text)
		if len(userMessage) > 500 {
			// Just an example limit
			updateChatData("assistant: Error: Message too long. Please limit to 500 characters or use the file upload.")
			return
		}
		if userMessage != "" {
			updateChatData("user: " + userMessage)
			messageInput.SetText("")
			go handleUserMessage()
		}
	}

	// Submitting the text (hitting Enter) calls sendMessage
	messageInput.OnSubmitted = func(content string) {
		sendMessage()
	}

	// A placeholder button for uploading a file (feature not fully implemented)
	uploadButton := widget.NewButton("+", func() {
		dialog.ShowInformation("File Upload", "Feature to upload files will be added.", myWindow)
	})

	// The "Send" button
	sendButton := widget.NewButton("Send", sendMessage)

	return messageInput, uploadButton, sendButton
}

// Assistant response tracking in global variables (for partial streaming updates).
var (
	assistantIndex  = -1            // Which index in chatData the partial text is being appended to
	assistantBubble *fyne.Container // Reference to the bubble container (not fully used in this snippet)
	assistantLabel  *widget.Label   // Reference to the label (not fully used in this snippet)
)

// createChatHistory builds a scrollable container that displays all messages in chatData.
func createChatHistory() *fyne.Container {
	chatContent := container.NewVBox()
	scroll = container.NewVScroll(chatContent)
	scroll.SetMinSize(fyne.NewSize(200, 300))

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

	return container.New(layout.NewStackLayout(), scroll)
}

// min is a small helper to get the smaller of two int values.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// handleUserMessage streams a response from Ollama (the LLM) based on the conversation so far.
func handleUserMessage() {
	if err := streamFromOllama(); err != nil {
		updateChatData("assistant: Error: " + fmt.Sprintf("%v", err))
	}
	saveCurrentChat()
}

// updateChatData appends a new message to the chatData binding.
func updateChatData(message string) {
	items, _ := chatData.Get()
	chatData.Set(append(items, message))
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

	var assistantMessageBuilder strings.Builder
	var assistantIndex = -1

	// 4) Define how to handle each partial response from Ollama
	respFunc := func(resp api.ChatResponse) error {
		assistantMessageBuilder.WriteString(resp.Message.Content)

		items, _ := chatData.Get()

		// If this is the first partial chunk for the assistant, append a new line to chatData
		if assistantIndex == -1 {
			newLine := "assistant: " + assistantMessageBuilder.String()
			items = append(items, newLine)
			chatData.Set(items)
			assistantIndex = len(items) - 1
		} else {
			// Otherwise, update the existing line in chatData
			updatedLine := "assistant: " + assistantMessageBuilder.String()
			items[assistantIndex] = updatedLine
			chatData.Set(items)
			// Rebuild chat history to reflect partial updates
			rebuildChatHistory()
		}

		// If the streaming is done, reset for next time
		if resp.Done {
			assistantMessageBuilder.Reset()
			assistantIndex = -1
		}
		return nil
	}

	// 5) Send the conversation to Ollama and stream responses
	if err := client.Chat(context.Background(), req, respFunc); err != nil {
		return fmt.Errorf("error in Ollama chat request: %w", err)
	}

	return nil
}
