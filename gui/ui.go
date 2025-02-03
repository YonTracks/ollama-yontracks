// ui.go
package main

import (
	"context"
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
	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/format"
)

const (
	defaultModel = "llama3.1"       // Example model name for Ollama
	httpTimeout  = 30 * time.Second // HTTP timeout for API calls
	appIconPath  = "app.ico"        // Application icon path
	sidebarRatio = 0.2              // Sidebar occupies 20% of width
)

var (
	currentModel  = defaultModel
	myApp         fyne.App
	myWindow      fyne.Window
	chatData      binding.StringList
	scroll        *container.Scroll
	currentChatID int
)

// initializeApp sets up the main Fyne application window.
func initializeApp() {
	myApp = app.NewWithID("ollama.gui")
	myWindow = myApp.NewWindow("Ollama GUI")

	if icon := loadAppIcon(appIconPath); icon != nil {
		myWindow.SetIcon(icon)
	}

	logLifecycle(myApp)
	createMenuBar()

	chatData = binding.NewStringList()
	ensureInitialChat()
	buildUI()

	myWindow.CenterOnScreen()
	myWindow.Resize(fyne.NewSize(800, 600))
	myWindow.ShowAndRun()
}

// ensureInitialChat checks if the DB is empty; if so, creates a "Welcome Chat".
func ensureInitialChat() {
	var chatCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM chats").Scan(&chatCount); err != nil {
		log.Printf("Failed to count chats: %v", err)
	}

	if chatCount == 0 {
		res, err := db.Exec("INSERT INTO chats (title, hash) VALUES (?, ?)", "Welcome Chat", "")
		if err != nil {
			log.Printf("Failed to create initial chat: %v", err)
			return
		}
		newID, _ := res.LastInsertId()
		currentChatID = int(newID)

		// Include the current model in the welcome message
		chatData.Set([]string{"assistant: Welcome to the chat! Current model: " + currentModel})
		saveCurrentChat()
	} else {
		var firstChatID int
		if err := db.QueryRow("SELECT id FROM chats ORDER BY id LIMIT 1").Scan(&firstChatID); err == nil {
			currentChatID = firstChatID
			loadChatHistory(firstChatID)
		}
	}
}

// buildUI constructs the main UI layout.
func buildUI() {
	chatsList, err := loadChatList()
	if err != nil {
		dialog.ShowError(err, myWindow)
		return
	}

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
					if err := deleteChat(chat.id); err != nil {
						dialog.ShowError(err, myWindow)
						return
					}
					updateSidebar()
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
	finalContent := container.NewBorder(nil, nil, nil, nil, mainUI)
	myWindow.SetContent(finalContent)
}

// displayModelList shows a list of downloaded models in a new window.
func displayModelList() {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		dialog.ShowError(err, myWindow)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	models, err := client.List(ctx)
	if err != nil {
		dialog.ShowError(err, myWindow)
		return
	}

	if len(models.Models) == 0 {
		dialog.ShowInformation("Models", "No models available.", myWindow)
		return
	}

	modelData := [][]string{
		{"Name", "Model", "Modified At", "Size", "Digest"},
	}

	for _, m := range models.Models {
		trimmedName := m.Name
		if idx := strings.Index(m.Name, ":"); idx != -1 {
			trimmedName = m.Name[:idx]
		}
		if len(trimmedName) > 16 {
			trimmedName = trimmedName[:15] + "…"
		}
		humanTime := format.HumanTime(m.ModifiedAt, "Unknown")
		humanSize := format.HumanBytes(m.Size)
		modelData = append(modelData, []string{
			trimmedName,
			m.Model,
			humanTime,
			humanSize,
			m.Digest,
		})
	}

	table := widget.NewTable(
		func() (int, int) { return len(modelData), len(modelData[0]) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			label.SetText(modelData[id.Row][id.Col])
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
			}
		},
	)
	table.SetColumnWidth(0, 200)
	table.SetColumnWidth(1, 350)
	table.SetColumnWidth(2, 150)
	table.SetColumnWidth(3, 100)
	table.SetColumnWidth(4, 450)

	scroll := container.NewVScroll(table)
	scroll.SetMinSize(fyne.NewSize(900, 400))

	heading := widget.NewLabel("Downloaded Models")
	heading.TextStyle = fyne.TextStyle{Bold: true}

	content := container.NewBorder(heading, nil, nil, nil, scroll)
	modelWindow := myApp.NewWindow("Model List")
	modelWindow.SetContent(content)
	modelWindow.Resize(fyne.NewSize(1000, 600))
	modelWindow.Show()
}

// displayRunningModels shows a list of running models.
func displayRunningModels() {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		dialog.ShowError(err, myWindow)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	runningModels, err := client.ListRunning(ctx)
	if err != nil {
		dialog.ShowError(err, myWindow)
		return
	}

	if len(runningModels.Models) == 0 {
		dialog.ShowInformation("Running Models", "No models are currently running.", myWindow)
		return
	}

	modelData := [][]string{
		{"Model", "Size", "Process", "Expires At"},
	}

	for _, m := range runningModels.Models {
		trimmedName := m.Name
		if idx := strings.Index(m.Name, ":"); idx != -1 {
			trimmedName = m.Name[:idx]
		}
		if len(trimmedName) > 16 {
			trimmedName = trimmedName[:15] + "…"
		}
		var processLabel string
		if m.Size == m.SizeVRAM {
			processLabel = "100% GPU"
		} else if m.SizeVRAM == 0 {
			processLabel = "100% CPU"
		} else {
			percentageGPU := (float64(m.SizeVRAM) / float64(m.Size)) * 100
			percentageCPU := 100 - percentageGPU
			processLabel = fmt.Sprintf("%.0f%% GPU / %.0f%% CPU", percentageGPU, percentageCPU)
		}
		humanSize := format.HumanBytes(m.Size)
		expiresAt := format.HumanTime(m.ExpiresAt, "Never")
		modelData = append(modelData, []string{
			m.Model,
			humanSize,
			processLabel,
			expiresAt,
		})
	}

	table := widget.NewTable(
		func() (int, int) { return len(modelData), len(modelData[0]) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.TableCellID, cell fyne.CanvasObject) {
			label := cell.(*widget.Label)
			label.SetText(modelData[id.Row][id.Col])
			if id.Row == 0 {
				label.TextStyle = fyne.TextStyle{Bold: true}
			}
		},
	)
	table.SetColumnWidth(0, 250)
	table.SetColumnWidth(1, 150)
	table.SetColumnWidth(2, 150)
	table.SetColumnWidth(3, 150)

	scroll := container.NewVScroll(table)
	scroll.SetMinSize(fyne.NewSize(800, 100))

	heading := widget.NewLabel("Running Models")
	heading.TextStyle = fyne.TextStyle{Bold: true}

	content := container.NewBorder(heading, nil, nil, nil, scroll)
	modelWindow := myApp.NewWindow("Running Models")
	modelWindow.SetContent(content)
	modelWindow.Resize(fyne.NewSize(900, 200))
	modelWindow.Show()
}

// createMenuBar constructs the top menu bar.
func createMenuBar() {
	themeToggle := fyne.NewMenuItem("Toggle Theme", func() {
		pref := myApp.Preferences()
		isDark := !pref.Bool("dark_mode")
		pref.SetBool("dark_mode", isDark)
		setTheme(isDark)
	})

	settingsMenu := fyne.NewMenu("Settings",
		fyne.NewMenuItem("Preferences", func() {
			dialog.ShowInformation("Preferences", "Settings menu under construction.", myWindow)
		}),
		fyne.NewMenuItem("About", func() {
			dialog.ShowInformation("About", "Ollama Chat App Version 1.0", myWindow)
		}),
	)
	modelsMenu := fyne.NewMenu("Models",
		fyne.NewMenuItem("Downloaded Models", displayModelList),
		fyne.NewMenuItem("Running Models", displayRunningModels),
		fyne.NewMenuItem("Download Model", func() {
			dialog.ShowInformation("Download Model", "Feature to download models will be added.", myWindow)
		}),
	)
	toolsMenu := fyne.NewMenu("Tools",
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
	)
	searchMenu := fyne.NewMenu("Search",
		fyne.NewMenuItem(fmt.Sprintf("Current Model: %s", defaultModel), func() {}),
		fyne.NewMenuItemSeparator(),
	)
	themeMenu := fyne.NewMenu("Theme", themeToggle)
	mainMenu := fyne.NewMainMenu(
		settingsMenu,
		modelsMenu,
		toolsMenu,
		searchMenu,
		themeMenu,
	)
	myWindow.SetMainMenu(mainMenu)
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

// makeMainUI sets up the main horizontal split between the sidebar and the chat pane.
func makeMainUI(serverList *widget.List) fyne.CanvasObject {
	chatHistory := createChatHistory()
	messageInput, uploadButton, sendButton := createInputComponents()
	inputContainer := container.NewBorder(nil, nil, uploadButton, sendButton, messageInput)
	messagePane := container.NewBorder(nil, inputContainer, nil, nil, chatHistory)
	mainContent := container.NewHSplit(serverList, messagePane)
	mainContent.SetOffset(sidebarRatio)
	return mainContent
}

// setTheme toggles between dark and light mode.
func setTheme(isDark bool) {
	if isDark {
		myApp.Settings().SetTheme(theme.DarkTheme())
	} else {
		myApp.Settings().SetTheme(theme.LightTheme())
	}
}

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
	bubble := container.NewStack(canvasWithBackgroundAndCenteredInput(label, isUser))
	if isUser {
		return container.NewHBox(layout.NewSpacer(), bubble)
	}
	return container.NewHBox(bubble, layout.NewSpacer())
}

// canvasWithBackgroundAndCenteredInput creates a rounded rectangle background for a chat bubble.
func canvasWithBackgroundAndCenteredInput(content fyne.CanvasObject, isUser bool) fyne.CanvasObject {
	var bgColor color.Color
	if isUser {
		bgColor = color.Gray{Y: 128}
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

// centeredContainer horizontally centers the content.
func centeredContainer(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(
		layout.NewSpacer(),
		container.New(layout.NewCenterLayout(), content),
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

	messageInput.OnSubmitted = func(content string) { sendMessage() }
	uploadButton := widget.NewButton("+", func() {
		dialog.ShowInformation("File Upload", "Feature to upload files will be added.", myWindow)
	})
	sendButton := widget.NewButton("Send", sendMessage)

	return messageInput, uploadButton, sendButton
}

// updateChatData appends a new message to chatData.
func updateChatData(message string) {
	items, _ := chatData.Get()
	chatData.Set(append(items, message))
	scroll.ScrollToBottom()
}

// updateSidebar refreshes the sidebar UI.
func updateSidebar() {
	buildUI()
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

	// Refresh the UI
	updateSidebar()
}

// handleSavedChatClick loads an existing chat.
func handleSavedChatClick(chatID int) {
	saveCurrentChat()
	currentChatID = chatID
	loadChatHistory(chatID)
	updateSidebar()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// loadAppIcon attempts to load an icon file from the given path.
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
