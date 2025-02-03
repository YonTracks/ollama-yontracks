// codeblock.go
package main

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// renderMarkdown takes a markdown–formatted string and returns a CanvasObject
// that renders normal text and code blocks. Code blocks are assumed to be
// delimited by triple backticks (```).
func renderMarkdown(message string) fyne.CanvasObject {
	// Split the message by the code block delimiter.
	parts := strings.Split(message, "```")
	// If there are no code blocks, simply return a plain label.
	if len(parts) == 1 {
		lbl := widget.NewLabel(message)
		lbl.Wrapping = fyne.TextWrapWord
		return lbl
	}

	// Create a vertical container that will hold the mixed content.
	var objects []fyne.CanvasObject
	for i, part := range parts {
		if i%2 == 0 {
			// Even–indexed parts are normal markdown text.
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				normalLabel := widget.NewLabel(trimmed)
				normalLabel.Wrapping = fyne.TextWrapWord
				objects = append(objects, normalLabel)
			}
		} else {
			// Odd–indexed parts are code blocks.
			codeLabel := widget.NewLabel(part)
			codeLabel.Wrapping = fyne.TextWrapWord
			codeLabel.TextStyle = fyne.TextStyle{Monospace: true}
			// Create a background rectangle for the code block based on the current theme.
			bg := canvas.NewRectangle(&color.Transparent)
			// Wrap the code label with some padding.
			codeContainer := container.NewMax(bg, container.NewPadded(codeLabel))
			objects = append(objects, codeContainer)
		}
	}
	return container.NewVBox(objects...)
}
