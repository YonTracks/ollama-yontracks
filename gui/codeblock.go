// codeblock.go
package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// renderMarkdown takes a markdown–formatted string and returns a CanvasObject
// that renders full markdown (including inline formatting and syntax–highlighted
// code blocks) using Fyne’s built–in markdown renderer.
func renderMarkdown(message string) fyne.CanvasObject {
	rt := widget.NewRichTextFromMarkdown(message)
	rt.Wrapping = fyne.TextWrapWord
	return rt
}
