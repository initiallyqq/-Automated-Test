package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Note struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

var db *sql.DB

func main() {
	var err error
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "test-app.db"
	}

	db, err = sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	schema, err := os.ReadFile("schema.sql")
	if err != nil {
		log.Fatalf("failed to read schema: %v", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		log.Fatalf("failed to run schema: %v", err)
	}

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/health", handleHealth)
	mux.HandleFunc("/api/notes", handleNotes)
	mux.HandleFunc("/api/notes/", handleNoteByID)

	// Serve static frontend
	mux.Handle("/", http.FileServer(http.Dir("static")))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	log.Printf("test-app server starting on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleNotes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listNotes(w, r)
	case http.MethodPost:
		createNote(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func handleNoteByID(w http.ResponseWriter, r *http.Request) {
	// /api/notes/123
	idStr := strings.TrimPrefix(r.URL.Path, "/api/notes/")
	if idStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing id"})
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	switch r.Method {
	case http.MethodPut:
		updateNote(w, r, id)
	case http.MethodDelete:
		deleteNote(w, r, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func listNotes(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query("SELECT id, title, content, status, created_at, updated_at FROM notes ORDER BY id DESC")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	notes := []Note{}
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.Title, &n.Content, &n.Status, &n.CreatedAt, &n.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		notes = append(notes, n)
	}
	writeJSON(w, http.StatusOK, notes)
}

func createNote(w http.ResponseWriter, r *http.Request) {
	var n Note
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if n.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title is required"})
		return
	}
	if n.Status == "" {
		n.Status = "todo"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		"INSERT INTO notes (title, content, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		n.Title, n.Content, n.Status, now, now,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	n.ID, _ = result.LastInsertId()
	n.CreatedAt = now
	n.UpdatedAt = now
	writeJSON(w, http.StatusCreated, n)
}

func updateNote(w http.ResponseWriter, r *http.Request, id int64) {
	var n Note
	if err := json.NewDecoder(r.Body).Decode(&n); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)
	result, err := db.Exec(
		"UPDATE notes SET title=?, content=?, status=?, updated_at=? WHERE id=?",
		n.Title, n.Content, n.Status, now, id,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
		return
	}

	n.ID = id
	n.UpdatedAt = now
	writeJSON(w, http.StatusOK, n)
}

func deleteNote(w http.ResponseWriter, r *http.Request, id int64) {
	result, err := db.Exec("DELETE FROM notes WHERE id=?", id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "note not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
