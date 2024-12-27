package main

import (
	"bytes"
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	// "fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/alecthomas/chroma/formatters/html"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
)

// createCodeBlock generates a syntax-highlighted code block as a Fyne canvas object.
func createCodeBlock(code, language, theme string) fyne.CanvasObject {
	// Get the lexer for the specified language
	lexer := lexers.Get(language)
	if lexer == nil {
		lexer = lexers.Fallback // Use fallback lexer if language is unsupported
	}

	// Get the theme/style
	style := styles.Get(theme)
	if style == nil {
		style = styles.Fallback // Use fallback style if theme is unsupported
	}

	// Format the code with syntax highlighting
	formatter := html.New(html.WithClasses(false))
	var buffer bytes.Buffer
	iterator, err := lexer.Tokenise(nil, code)
	if err == nil {
		err = formatter.Format(&buffer, style, iterator)
	}
	if err != nil {
		return widget.NewLabel(fmt.Sprintf("Error formatting code: %v", err))
	}

	// Render the HTML output as a label (basic rendering)
	htmlOutput := buffer.String()

	// Convert the HTML to a basic Fyne-friendly string
	// Note: Fyne's RichText widget does not directly support HTML, so we parse to plain text
	plainText := stripHTML(htmlOutput)
	label := widget.NewLabel(plainText)
	label.Wrapping = fyne.TextWrapWord

	// Wrap the label in a bordered container for a "code block" feel
	return container.NewVBox(
		// canvas.NewRectangle(&fyne.Color{R: 240, G: 240, B: 240, A: 255}), // light gray background
		label,
	)
}

// stripHTML removes HTML tags to extract plain text (basic functionality).
func stripHTML(input string) string {
	output := strings.NewReplacer(
		"<br>", "\n",
		"<b>", "",
		"</b>", "",
		"<i>", "",
		"</i>", "",
		"<u>", "",
		"</u>", "",
		"<div>", "",
		"</div>", "",
		"<span>", "",
		"</span>", "",
	).Replace(input)

	return output
}
