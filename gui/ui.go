// ui.go
package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/cmd/fyne_settings/settings"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// createMenuBar constructs the top menu bar.
func createMenuBar() {
	// Toggle the theme between dark and light.
	themeToggle := fyne.NewMenuItem("Toggle Theme", func() {
		pref := myApp.Preferences()
		isDark := !pref.Bool("dark_mode")
		pref.SetBool("dark_mode", isDark)
		setTheme(isDark)
	})

	// Settings menu: open Preferences or show About information.
	settingsMenu := fyne.NewMenu("Settings",
		fyne.NewMenuItem("Preferences", func() {
			w := myApp.NewWindow("Fyne Settings")
			w.SetContent(settings.NewSettings().LoadAppearanceScreen(w))
			w.Resize(fyne.NewSize(800, 600))
			w.Show()
		}),
		fyne.NewMenuItem("About", func() {
			dialog.ShowInformation("About", "Ollama Chat App Version 1.0", myWindow)
		}),
	)

	// Models menu: list downloaded/running models or start a download.
	modelsMenu := fyne.NewMenu("Models",
		fyne.NewMenuItem("Downloaded Models", createModelList),
		fyne.NewMenuItem("Running Models", displayRunningModels),
		fyne.NewMenuItem("Download Model", func() {
			dialog.ShowInformation("Download Model", "Feature to download models will be added.", myWindow)
		}),
	)

	// Tools menu: placeholder items for future functionality.
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

	// Search menu: display current model.
	searchMenu := fyne.NewMenu("Search",
		fyne.NewMenuItem(fmt.Sprintf("Current Model: %s", defaultModel), func() {}),
		fyne.NewMenuItemSeparator(),
	)

	// Theme menu simply contains the theme toggle.
	themeMenu := fyne.NewMenu("Theme", themeToggle)

	// Build the main menu and attach it to the window.
	mainMenu := fyne.NewMainMenu(
		settingsMenu,
		modelsMenu,
		toolsMenu,
		searchMenu,
		themeMenu,
	)
	myWindow.SetMainMenu(mainMenu)
}

// makeMainUI sets up the main horizontal split between the sidebar and the chat pane.
func makeMainUI(serverList *widget.List) fyne.CanvasObject {
	chatHistory := createChatHistory()
	messageInput, uploadButton, sendButton := createInputComponents()

	// Arrange the input components: upload button on the left, send button on the right.
	inputContainer := container.NewBorder(nil, nil, uploadButton, sendButton, messageInput)
	// Place the chat history above the input container.
	messagePane := container.NewBorder(nil, inputContainer, nil, nil, chatHistory)

	// Split the UI horizontally between the server list (sidebar) and the chat pane.
	mainContent := container.NewHSplit(serverList, messagePane)
	mainContent.SetOffset(sidebarRatio) // sidebarRatio is assumed defined globally.
	return mainContent
}

// makeSidebar constructs the sidebar UI layout.
func makeSidebar() {
	chatsList, err := loadChatList()
	if err != nil {
		dialog.ShowError(err, myWindow)
		return
	}

	// Create the chat list widget. The first item (id == 0) is reserved for a new chat.
	serverList := widget.NewList(
		func() int {
			return len(chatsList) + 1
		},
		// Create a new list item: an HBox with a label on the left and a delete button on the right.
		func() fyne.CanvasObject {
			label := widget.NewLabel("")
			deleteButton := widget.NewButtonWithIcon("", theme.DeleteIcon(), nil)
			// Use a spacer between the label and delete button.
			return container.NewHBox(label, layout.NewSpacer(), deleteButton)
		},
		// Update each list item based on its id.
		func(id widget.ListItemID, o fyne.CanvasObject) {
			// Since we built the item as an HBox, we know:
			// - index 0 is the label, and index 2 is the delete button.
			hbox := o.(*fyne.Container)
			label, ok := hbox.Objects[0].(*widget.Label)
			if !ok {
				return
			}
			deleteBtn, ok := hbox.Objects[2].(*widget.Button)
			if !ok {
				return
			}

			// The first item creates a new chat.
			if id == 0 {
				label.SetText("New Chat")
				deleteBtn.Hide()
				deleteBtn.OnTapped = nil
			} else {
				chat := chatsList[id-1]
				label.SetText(chat.title)
				deleteBtn.Show()
				// When the delete button is tapped, delete the chat and refresh the sidebar.
				deleteBtn.OnTapped = func() {
					if err := deleteChat(chat.id); err != nil {
						dialog.ShowError(err, myWindow)
						return
					}
					makeSidebar()
				}
			}
		},
	)

	// Define what happens when a list item is selected.
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
