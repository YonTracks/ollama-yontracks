// gui.go
package main

import "log"

func main() {
	log.Println("Initializing database...")
	err := initializeDB()
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer db.Close()
	log.Println("Database initialized.")

	log.Println("Starting GUI application...")
	initializeApp()
}
