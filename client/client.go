package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/html"
	"github.com/gomarkdown/markdown/parser"
)

// Note struct represents a note
type Note struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

var (
	serverURL = "https://divy-livenotes.fly.dev"
	username  = "divy" // Change this to your server username
	password  = ""     // Change this to your server password
)

func main() {
	if len(os.Args) != 5 {
		fmt.Println("Usage: go run client.go <filename> <username> <password> <serverURL>")
		return
	}

	filename := os.Args[1]
	// Do not sync .notes_config file
	if strings.HasSuffix(filename, ".notes_config") {
		return
	}

	username = os.Args[2]
	password = os.Args[3]
	serverURL = os.Args[4]

	fileContents, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal("Error reading file:", err)
	}

	fileStr := string(fileContents)
	addOrUpdateNote(filename, fileStr)
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

// Function to add or update a note on the server
func addOrUpdateNote(title, body string) {
	note := Note{
		Title: title,
		Body:  body,
	}

	endpoint := "/sync"
	// If filename has no extension or .md extension, use /sync endpoint
	if !strings.Contains(title, ".") || strings.HasSuffix(title, ".md") {
		note.Body = string(mdToHTML([]byte(body)))
	}

	// Marshal note into JSON
	jsonData, err := json.Marshal(note)
	if err != nil {
		log.Fatal("Error marshaling JSON:", err)
	}

	// Create HTTP client with basic authentication
	client := &http.Client{}
	req, err := http.NewRequest("POST", serverURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatal("Error creating request:", err)
	}
	req.SetBasicAuth(username, password)
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Error sending request:", err)
	}
	defer resp.Body.Close()

	// Read response
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Error reading response:", err)
	}

	fmt.Println("Response:", resp.Status)
	fmt.Println("Response Body:", string(bodyBytes))
}
