// db.go
package main

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// Constants related to the database
const dbFile = "ollama_chats.db"

// Global variables for database management (could be wrapped in a struct)
var (
	db *sql.DB
	mu sync.Mutex
)

// chatRecord holds a single row from the "chats" table.
type chatRecord struct {
	id    int
	title string
	hash  string
}

// initializeDB sets up the SQLite database (opens the connection and creates tables if needed).
func initializeDB() error {
	if !fileExists(dbFile) {
		log.Printf("Database file %s does not exist. It will be created.", dbFile)
	}

	var err error
	db, err = sql.Open("sqlite3", dbFile)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := execSQL(`
		CREATE TABLE IF NOT EXISTS chats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			hash TEXT NOT NULL
		);
	`); err != nil {
		return err
	}

	if err := execSQL(`
		CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
			content TEXT NOT NULL,
			FOREIGN KEY(chat_id) REFERENCES chats(id) ON DELETE CASCADE
		);
	`); err != nil {
		return err
	}

	// Optionally, do a test query to ensure DB is really connected
	if err = db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

// execSQL is a helper function to run a SQL statement (used for setup).
func execSQL(query string) error {
	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to execute SQL:\n%s\nError: %w", query, err)
	}
	return nil
}

// fileExists checks if a file exists at the given path.
func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

// insertChat creates a new record in the "chats" table, setting currentChatID to that new ID.
func insertChat(title string, hash string) error {
	stmt, err := db.Prepare("INSERT INTO chats (title, hash) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("Failed to prepare insert chat statement: %w", err)
	}
	defer stmt.Close()

	res, err := stmt.Exec(title, hash)
	if err != nil {
		return fmt.Errorf("Failed to insert new chat: %w", err)
	}
	newID, _ := res.LastInsertId()
	currentChatID = int(newID)
	return nil
}

// updateChatHash updates the stored hash for the current chat record.
func updateChatHash(hash string) error {
	stmt, err := db.Prepare("UPDATE chats SET hash = ? WHERE id = ?")
	if err != nil {
		return fmt.Errorf("Failed to prepare update chat hash statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(hash, currentChatID)
	if err != nil {
		return fmt.Errorf("Failed to update chat hash: %w", err)
	}
	return nil
}

// deleteOldMessages removes all messages belonging to a particular chat ID.
//
// NOTE: We do NOT call updateSidebar() here to avoid mixing DB code and UI logic.
func deleteOldMessages(chatID int) error {
	stmt, err := db.Prepare("DELETE FROM messages WHERE chat_id = ?")
	if err != nil {
		return fmt.Errorf("Failed to prepare delete messages statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(chatID)
	if err != nil {
		return fmt.Errorf("Failed to delete old messages: %w", err)
	}
	return nil
}

// insertMessages writes a slice of message strings to the database for the current chat.
func insertMessages(messages []string) error {
	stmt, err := db.Prepare("INSERT INTO messages (chat_id, role, content) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("Failed to prepare insert messages statement: %w", err)
	}
	defer stmt.Close()

	for _, line := range messages {
		role, content := parseRoleAndContent(line)
		if !isValidRole(role) {
			log.Printf("Invalid role detected: %s", role)
			continue
		}
		_, err := stmt.Exec(currentChatID, role, content)
		if err != nil {
			log.Printf("Failed to insert message: %v", err)
		}
	}
	return nil
}

// isValidRole ensures the role is either "user" or "assistant".
func isValidRole(role string) bool {
	return role == "user" || role == "assistant"
}

// hashChat creates a SHA-256 hash of the entire chat content.
func hashChat(chat []string) string {
	hash := sha256.New()
	for _, line := range chat {
		hash.Write([]byte(line))
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// getCurrentChat retrieves the messages from the chatData binding as a slice of strings.
func getCurrentChat() []string {
	items, _ := chatData.Get()
	return items
}

// loadChatHistory pulls all messages for a given chat ID from the DB and loads them into chatData.
func loadChatHistory(chatID int) {
	mu.Lock()
	defer mu.Unlock()

	stmt, err := db.Prepare("SELECT role, content FROM messages WHERE chat_id = ?")
	if err != nil {
		log.Printf("Failed to prepare SELECT statement for messages: %v", err)
		return
	}
	defer stmt.Close()

	rows, err := stmt.Query(chatID)
	if err != nil {
		log.Printf("Failed to load chat messages: %v", err)
		return
	}
	defer rows.Close()

	var messages []string
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			log.Printf("Error scanning message: %v", err)
			continue
		}
		// Combine them as "role: content"
		messages = append(messages, role+": "+content)
	}
	chatData.Set(messages)
}

// getNextChatNumber gets the number of existing chats, so we can name new chats accordingly (e.g., Chat 1, Chat 2...).
func getNextChatNumber() int {
	var count int
	stmt, err := db.Prepare("SELECT COUNT(*) FROM chats")
	if err != nil {
		log.Printf("Failed to prepare count chats statement: %v", err)
		return 1
	}
	defer stmt.Close()

	err = stmt.QueryRow().Scan(&count)
	if err != nil {
		log.Printf("Failed to count chats: %v", err)
		return 1
	}
	return count + 1
}

// loadChatList retrieves all chats from the database (for sidebar display).
func loadChatList() ([]chatRecord, error) {
	rows, err := db.Query("SELECT id, title, hash FROM chats ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("Failed to select from chats: %w", err)
	}
	defer rows.Close()

	var result []chatRecord
	for rows.Next() {
		var r chatRecord
		if err := rows.Scan(&r.id, &r.title, &r.hash); err != nil {
			log.Printf("Error scanning chat list: %v", err)
			continue
		}
		result = append(result, r)
	}
	return result, nil
}

// saveCurrentChat performs several actions to persist the current in-memory chat data to the database:
// 1. Checks if the current chat is new or existing (by checking a hash).
// 2. Updates or inserts the chat record in the DB if needed.
// 3. Deletes old messages for the current chat (to replace them).
// 4. Inserts new messages from the memory binding (chatData).
//
// NOTE: The UI layer is responsible for calling updateSidebar() afterwards if needed.
func saveCurrentChat() {
	mu.Lock()
	defer mu.Unlock()

	if currentChatID < 0 {
		// Means there's no valid "current" chat
		return
	}

	currentChat := getCurrentChat()
	if len(currentChat) == 0 {
		// Nothing to save if there are no messages
		return
	}

	hash := hashChat(currentChat)

	// See if we already have a stored hash for this chat
	stmt, err := db.Prepare("SELECT hash FROM chats WHERE id = ?")
	if err != nil {
		log.Printf("Failed to prepare SELECT statement for chat hash: %v", err)
		return
	}
	defer stmt.Close()

	var existingHash string
	err = stmt.QueryRow(currentChatID).Scan(&existingHash)
	if err == sql.ErrNoRows {
		// If there is no chat record, create a new one
		title := fmt.Sprintf("Chat %d", getNextChatNumber())
		if err := insertChat(title, hash); err != nil {
			log.Printf("Failed to insert new chat: %v", err)
			return
		}
	} else if err != nil {
		log.Printf("Error checking chat: %v", err)
	} else if existingHash != hash {
		// If the hashes differ, update it
		if err := updateChatHash(hash); err != nil {
			log.Printf("Failed to update chat hash: %v", err)
		}
	}

	// Clear out old messages and insert fresh ones
	if err := deleteOldMessages(currentChatID); err != nil {
		log.Printf("Failed to delete old messages: %v", err)
	}
	if err := insertMessages(currentChat); err != nil {
		log.Printf("Failed to insert messages: %v", err)
	}
}

// deleteChat removes the chat record and messages from the database, and resets the current chat if needed.
//
// The UI layer must call updateSidebar() after this function if needed.
func deleteChat(chatID int) error {
	stmt, err := db.Prepare("DELETE FROM chats WHERE id = ?")
	if err != nil {
		return fmt.Errorf("Failed to prepare delete chat statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(chatID)
	if err != nil {
		return fmt.Errorf("Failed to delete chat: %w", err)
	}

	if chatID == currentChatID {
		chatsList, _ := loadChatList()
		if len(chatsList) > 0 {
			currentChatID = chatsList[0].id
			loadChatHistory(currentChatID)
		} else {
			currentChatID = -1
			chatData.Set([]string{})
		}
	}
	return nil
}
