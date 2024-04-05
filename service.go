package main

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
	"github.com/joho/godotenv"
	"golang.org/x/crypto/bcrypt"
)

// Note struct represents a note
type Note struct {
	Title string `json:"title"`
	Body  string `json:"body"`
  Shared bool `json:"shared"`
}

var (
	notes        []Note
	notesLock    sync.Mutex
	filename     = "/var/notes/notes.json"
	username     string
	passwordHash []byte
)

func main() {
	err := godotenv.Load()
	if err != nil {
		// pass
	}

	username = os.Getenv("USERNAME")
	password := os.Getenv("PASSWORD")
	passwordHash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		log.Fatal("Error generating password hash:", err)
	}

	loadNotes()

	http.HandleFunc("/", authMiddleware(listNotesHandler, username, passwordHash))
	http.HandleFunc("/sync", authMiddleware(syncNoteHandler, username, passwordHash))
  http.HandleFunc("/delete", authMiddleware(deleteNoteHandler, username, passwordHash))
  http.HandleFunc("/share", authMiddleware(shareNoteHandler, username, passwordHash))
  http.HandleFunc("/x/", readNoteHandler)

	fmt.Println("Server is running on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Load notes from file
func loadNotes() {
	file, err := os.Open(filename)
	defer file.Close()
	if err != nil {
		fmt.Println("No existing notes found, starting fresh.")
		os.MkdirAll("/var/notes", os.ModePerm)
		return
	}

	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&notes); err != nil {
		log.Fatal("Error decoding notes:", err)
	}
	fmt.Println("Notes loaded successfully.")
}

// Save notes to file
func saveNotes() {
	file, err := os.Create(filename)
	defer file.Close()
	if err != nil {
		log.Fatal("Error creating file:", err)
	}

	encoder := json.NewEncoder(file)
	if err := encoder.Encode(notes); err != nil {
		log.Fatal("Error encoding notes:", err)
	}
}

func mdToHTML(md []byte) []byte {
	// create markdown parser with extensions
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.NoEmptyLineBeforeBlock
	p := parser.NewWithExtensions(extensions)
	doc := p.Parse(md)

	// create HTML renderer with extensions
	htmlFlags := html.CommonFlags | html.HrefTargetBlank
	opts := html.RendererOptions{Flags: htmlFlags}
	renderer := html.NewRenderer(opts)

	return markdown.Render(doc, renderer)
}

// Handler to list all notes
func listNotesHandler(w http.ResponseWriter, r *http.Request) {
	notesLock.Lock()
	defer notesLock.Unlock()

	fmt.Fprintf(w, "<html><head><link rel='stylesheet' href='https://divy.work/tufte.css'></head><body><article>")
	for _, note := range notes {
		fmt.Fprintf(w, "<h1>%s</h1><hr>%s", note.Title, mdToHTML([]byte(note.Body)))
	}
	fmt.Fprintf(w, "</article></body></html>")
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
}

// Handler for sharing a note
func shareNoteHandler(w http.ResponseWriter, r *http.Request) {
  notesLock.Lock()
  defer notesLock.Unlock()

  title := r.URL.Query().Get("title")
  if title == "" {
    http.Error(w, "Missing title query parameter", http.StatusBadRequest)
    return
  }

  var shared bool
  for i, note := range notes {
    if note.Title == title {
      notes[i].Shared = true
      shared = true
      break
    }
  }

  if !shared {
    http.Error(w, "Note not found", http.StatusNotFound)
    return
  }

  saveNotes()

  w.WriteHeader(http.StatusOK)
  fmt.Fprintf(w, "Note shared: %s", title)
}

// Handler to delete a note
func deleteNoteHandler(w http.ResponseWriter, r *http.Request) {
  notesLock.Lock()
  defer notesLock.Unlock()

  title := r.URL.Query().Get("title")
  if title == "" {
    http.Error(w, "Missing title query parameter", http.StatusBadRequest)
    return
  }

  var deleted bool
  for i, note := range notes {
    if note.Title == title {
      notes = append(notes[:i], notes[i+1:]...)
      deleted = true
      break
    }
  }

  if !deleted {
    http.Error(w, "Note not found", http.StatusNotFound)
    return
  }

  saveNotes()

  w.WriteHeader(http.StatusOK)
  fmt.Fprintf(w, "Note deleted: %s", title)
}

// Handler to add or update a note
func syncNoteHandler(w http.ResponseWriter, r *http.Request) {
	notesLock.Lock()
	defer notesLock.Unlock()

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var updatedNote Note
	if err := json.Unmarshal(body, &updatedNote); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Check if the note already exists
	var exists bool
	for i, note := range notes {
		if note.Title == updatedNote.Title {
			notes[i].Body = updatedNote.Body
			exists = true
			break
		}
	}

	// If the note does not exist, add it
	if !exists {
		notes = append(notes, updatedNote)
	}

	saveNotes()

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Note synced: %s", updatedNote.Title)
}

func readNoteHandler(w http.ResponseWriter, r *http.Request) {
  title := r.URL.Path[3:]
  fmt.Println("Title:", title)
  if title == "" {
    http.Error(w, "Missing title query parameter", http.StatusBadRequest)
    return
  }

  notesLock.Lock()
  defer notesLock.Unlock()

  for _, note := range notes {
    if note.Title == title {
      if note.Shared {
        fmt.Fprintf(w, "<html><head><link rel='stylesheet' href='https://divy.work/tufte.css'></head><body><article><h1>%s</h1><hr>%s</article></body></html>", note.Title, mdToHTML([]byte(note.Body)))
        w.Header().Set("Content-Type", "text/html")
        w.WriteHeader(http.StatusOK)
        return
      } else {
        user, pass, ok := r.BasicAuth()
        if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 || !checkPassword(pass, passwordHash) {
          w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
          http.Error(w, "Unauthorized", http.StatusUnauthorized)
          return
        }

        fmt.Fprintf(w, "<html><head><link rel='stylesheet' href='https://divy.work/tufte.css'></head><body><article><h1>%s</h1><hr>%s</article></body></html>", note.Title, mdToHTML([]byte(note.Body)))
        w.Header().Set("Content-Type", "text/html")
        w.WriteHeader(http.StatusOK)
        return
      }
    }

    http.Error(w, "Note not found", http.StatusNotFound)
    return
  }
}

// Middleware to enforce HTTP basic authentication
func authMiddleware(next http.HandlerFunc, username string, passwordHash []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 || !checkPassword(pass, passwordHash) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	}
}

// Function to check if the provided password matches the stored hash
func checkPassword(password string, hash []byte) bool {
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	return err == nil
}
