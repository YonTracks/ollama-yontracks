// theme.go
package main

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// forcedVariant forces a particular theme variant (light or dark)
// by wrapping an existing theme.
type forcedVariant struct {
	fyne.Theme
	variant fyne.ThemeVariant
}

// Color returns the color for a theme element using the forced variant.
func (f *forcedVariant) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return f.Theme.Color(name, f.variant)
}

// setTheme toggles between dark and light mode.
func setTheme(isDark bool) {
	if isDark {
		myApp.Settings().SetTheme(&forcedVariant{Theme: theme.DefaultTheme(), variant: theme.VariantLight})

		isDark = false
	} else {
		myApp.Settings().SetTheme(&forcedVariant{Theme: theme.DefaultTheme(), variant: theme.VariantDark})

		isDark = true
	}
}
