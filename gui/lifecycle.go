// lifecycle.go
package main

import (
	"log"

	"fyne.io/fyne/v2"
)

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
