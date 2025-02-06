// ui.go
package main

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/cmd/fyne_settings/settings"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

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
			w := myApp.NewWindow("Fyne Settings")
			w.SetContent(settings.NewSettings().LoadAppearanceScreen(w))
			w.Resize(fyne.NewSize(800, 600))
			w.Show()
		}),
		fyne.NewMenuItem("About", func() {
			dialog.ShowInformation("About", "Ollama Chat App Version 1.0", myWindow)
		}),
	)
	modelsMenu := fyne.NewMenu("Models",
		fyne.NewMenuItem("Downloaded Models", createModelList),
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

// makeSidebar constructs the main UI layout.
func makeSidebar() {
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
					makeSidebar()
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
