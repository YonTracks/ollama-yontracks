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

const dbFile = "ollama_chats.db"

var (
	db *sql.DB
	mu sync.Mutex
)

type chatRecord struct {
	id    int
	title string
	hash  string
}

// initializeDB opens the database connection and creates necessary tables.
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

	if err = db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	return nil
}

func execSQL(query string) error {
	_, err := db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to execute SQL:\n%s\nError: %w", query, err)
	}
	return nil
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

func insertChat(title, hash string) error {
	stmt, err := db.Prepare("INSERT INTO chats (title, hash) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare insert chat statement: %w", err)
	}
	defer stmt.Close()

	res, err := stmt.Exec(title, hash)
	if err != nil {
		return fmt.Errorf("failed to insert new chat: %w", err)
	}
	newID, _ := res.LastInsertId()
	currentChatID = int(newID)
	return nil
}

func updateChatHash(hash string) error {
	stmt, err := db.Prepare("UPDATE chats SET hash = ? WHERE id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare update chat hash statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(hash, currentChatID)
	if err != nil {
		return fmt.Errorf("failed to update chat hash: %w", err)
	}
	return nil
}

func deleteOldMessages(chatID int) error {
	stmt, err := db.Prepare("DELETE FROM messages WHERE chat_id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare delete messages statement: %w", err)
	}
	defer stmt.Close()

	_, err = stmt.Exec(chatID)
	if err != nil {
		return fmt.Errorf("failed to delete old messages: %w", err)
	}
	return nil
}

func insertMessages(messages []string) error {
	stmt, err := db.Prepare("INSERT INTO messages (chat_id, role, content) VALUES (?, ?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare insert messages statement: %w", err)
	}
	defer stmt.Close()

	for _, line := range messages {
		role, content := parseRoleAndContent(line)
		if !isValidRole(role) {
			log.Printf("Invalid role detected: %s", role)
			continue
		}
		if _, err := stmt.Exec(currentChatID, role, content); err != nil {
			log.Printf("Failed to insert message: %v", err)
		}
	}
	return nil
}

func isValidRole(role string) bool {
	return role == "user" || role == "assistant"
}

func hashChat(chat []string) string {
	hash := sha256.New()
	for _, line := range chat {
		hash.Write([]byte(line))
	}
	return fmt.Sprintf("%x", hash.Sum(nil))
}

func getCurrentChat() []string {
	items, _ := chatData.Get()
	return items
}

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
		messages = append(messages, role+": "+content)
	}
	chatData.Set(messages)
}

func getNextChatNumber() int {
	var count int
	stmt, err := db.Prepare("SELECT COUNT(*) FROM chats")
	if err != nil {
		log.Printf("Failed to prepare count chats statement: %v", err)
		return 1
	}
	defer stmt.Close()

	if err := stmt.QueryRow().Scan(&count); err != nil {
		log.Printf("Failed to count chats: %v", err)
		return 1
	}
	return count + 1
}

func loadChatList() ([]chatRecord, error) {
	rows, err := db.Query("SELECT id, title, hash FROM chats ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("failed to select from chats: %w", err)
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

// saveCurrentChat persists the in–memory chat data to the database.
func saveCurrentChat() {
	mu.Lock()
	defer mu.Unlock()

	if currentChatID < 0 {
		return
	}

	currentChat := getCurrentChat()
	if len(currentChat) == 0 {
		return
	}

	hash := hashChat(currentChat)

	stmt, err := db.Prepare("SELECT hash FROM chats WHERE id = ?")
	if err != nil {
		log.Printf("Failed to prepare SELECT statement for chat hash: %v", err)
		return
	}
	defer stmt.Close()

	var existingHash string
	err = stmt.QueryRow(currentChatID).Scan(&existingHash)
	if err == sql.ErrNoRows {
		title := fmt.Sprintf("Chat %d", getNextChatNumber())
		if err := insertChat(title, hash); err != nil {
			log.Printf("Failed to insert new chat: %v", err)
			return
		}
	} else if err != nil {
		log.Printf("Error checking chat: %v", err)
	} else if existingHash != hash {
		if err := updateChatHash(hash); err != nil {
			log.Printf("Failed to update chat hash: %v", err)
		}
	}

	if err := deleteOldMessages(currentChatID); err != nil {
		log.Printf("Failed to delete old messages: %v", err)
	}
	if err := insertMessages(currentChat); err != nil {
		log.Printf("Failed to insert messages: %v", err)
	}
}

// deleteChat removes a chat record and its messages from the database.
func deleteChat(chatID int) error {
	stmt, err := db.Prepare("DELETE FROM chats WHERE id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare delete chat statement: %w", err)
	}
	defer stmt.Close()

	if _, err := stmt.Exec(chatID); err != nil {
		return fmt.Errorf("failed to delete chat: %w", err)
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
