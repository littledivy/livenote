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
	Title  string `json:"title"`
	Body   string `json:"body"`
	Shared bool   `json:"shared"`
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

	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/sync", authMiddleware(syncNoteHandler, username, passwordHash))
  http.HandleFunc("/sync-raw", authMiddleware(syncNoteRawHandler, username, passwordHash))
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

// Handler for the home page
func homeHandler(w http.ResponseWriter, r *http.Request) {
	notesLock.Lock()
	defer notesLock.Unlock()

	fmt.Fprintf(w, "<html><head><meta name='viewport' content='width=device-width, initial-scale=1'><link rel='stylesheet' href='https://divy.work/tufte.css'></head><body><article>")

	// Details about server and number of notes.
	fmt.Fprintf(w, "<h2>Welcome to livenote</h2>")

	fmt.Fprintf(w, "<pre><code>")
	fmt.Fprintf(w, "<p>Instance host: %s</p>", r.Host)
	fmt.Fprintf(w, "<p>Notes: %d</p>", len(notes))
	file, err := os.Open(filename)
	defer file.Close()
	if err != nil {
		fmt.Fprintf(w, "<p>Storage not available</p>")
	} else {
		fi, _ := file.Stat()
		fmt.Fprintf(w, "<p>Space used: %d KB / %d KB</p>", fi.Size()/1024, 2*1024*1024)
	}
	fmt.Fprintf(w, "</code></pre>")

	fmt.Fprintf(w, "<footer><p><a href='https://github.com/littledivy/livenote'>Host your own</a></a></p></footer>")

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
  syncNoteHandlerInner(w, r, false)
}

func syncNoteRawHandler(w http.ResponseWriter, r *http.Request) {
  syncNoteHandlerInner(w, r, true)
}

func syncNoteHandlerInner(w http.ResponseWriter, r *http.Request, raw bool) {
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
      if !raw {
        notes[i].Body = string(mdToHTML([]byte(updatedNote.Body)))
      } else {
        notes[i].Body = updatedNote.Body
      }
			exists = true
			break
		}
	}

	// If the note does not exist, add it
	if !exists {
		note := Note{
      Title:  updatedNote.Title,
      Body:   updatedNote.Body,
      Shared: false,
    }
    if !raw {
      note.Body = string(mdToHTML([]byte(updatedNote.Body)))
    }

    notes = append(notes, note)
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
        renderNoteHTML(w, note)
				return
			} else {
				user, pass, ok := r.BasicAuth()
				if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 || !checkPassword(pass, passwordHash) {
					w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
					http.Error(w, "Unauthorized", http.StatusUnauthorized)
					return
				}

        renderNoteHTML(w, note)
				return
			}
		}
	}

	http.Error(w, "Note not found", http.StatusNotFound)
	return
}

func renderNoteHTML(w http.ResponseWriter, note Note) {
  s := `
<html>
  <head>
    <meta name='viewport' content='width=device-width, initial-scale=1'>
    <link rel='stylesheet' href='https://divy.work/tufte.css'>
  </head>
  <body>
    <article>
      <h1>%s</h1>
      <hr>
      <div contenteditable style='outline: none;'>
        %s
      </div>
    </article>
    <script>
      const saveNote = (title, body) => {
        fetch('/sync-raw', {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({
            title: title,
            body: body,
          }),
        });
      };

      const debounce = (func, delay) => {
        let inDebounce;
        return function() {
          const context = this;
          const args = arguments;
          clearTimeout(inDebounce);
          inDebounce = setTimeout(() => func.apply(context, args), delay);

          document.querySelector('body').style.cursor = 'wait';
          setTimeout(() => {
            document.querySelector('body').style.cursor = 'auto';
          }, delay);

          const savedMessage = document.createElement('div');
          savedMessage.innerHTML = 'Saving';
          savedMessage.style.position = 'fixed';
          savedMessage.style.bottom = '10px';
          savedMessage.style.right = '10px';
          savedMessage.style.backgroundColor = 'black';
          savedMessage.style.color = 'white';
          savedMessage.style.padding = '10px';
          savedMessage.style.borderRadius = '5px';
          document.body.appendChild(savedMessage);
          setTimeout(() => {
            savedMessage.remove();
          }, 2000);
        };
      };

      const noteTitle = '%s';
      const noteBody = document.querySelector('div[contenteditable]');
      noteBody.addEventListener('input', debounce(() => {
        saveNote(noteTitle, noteBody.innerHTML);
      }, 2000));
    </script>
  </body>
</html>`
  fmt.Fprintf(w, s, note.Title, note.Body, note.Title)
  w.Header().Set("Content-Type", "text/html")
  w.WriteHeader(http.StatusOK)
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
