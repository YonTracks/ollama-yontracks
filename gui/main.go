// main.go
package main

import (
	"log"
)

func main() {
	log.Println("Initializing database...")
	if err := initializeDB(); err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing database: %v", err)
		}
	}()
	log.Println("Database initialized.")

	log.Println("Starting GUI application...")
	initializeApp()
}
