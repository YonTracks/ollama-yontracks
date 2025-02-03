// model.go
package main

import (
	"context"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/ollama/ollama/api"
	"github.com/ollama/ollama/format"
)

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

// createModelList displays a list of downloaded models.
func createModelList() {
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
