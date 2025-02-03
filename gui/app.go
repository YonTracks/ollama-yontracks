// app.go
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
	"fyne.io/fyne/v2/widget"
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

// logLifecycle logs key lifecycle events of the Fyne app.
func logLifecycle(app fyne.App) {
	log.Println("Setting up lifecycle logging...")
	lifecycle := app.Lifecycle()
	lifecycle.SetOnStarted(func() {
		log.Println("Lifecycle: Started")
	})
	lifecycle.SetOnStopped(func() {
		log.Println("Lifecycle: Stopped")
	})
	lifecycle.SetOnEnteredForeground(func() {
		log.Println("Lifecycle: Entered Foreground")
	})
	lifecycle.SetOnExitedForeground(func() {
		log.Println("Lifecycle: Exited Foreground")
	})
}
