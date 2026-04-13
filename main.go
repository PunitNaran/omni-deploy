package main

import (
	"fmt"
	"log"
	"os"
	"net/http"
	"github.com/joho/godotenv"
)

// Load configuration from .env file
func loadConfig() (string, error) {
	if err := godotenv.Load(); err != nil {
		return "", fmt.Errorf("Error loading .env file: %v", err)
	}
	return os.Getenv("APP_NAME"), nil
}

// ValidateInput checks if the input is valid
func ValidateInput(input string) bool {
	// Add your validation logic here
	return len(input) > 0
}

// Handler function to respond to HTTP requests
func handler(w http.ResponseWriter, r *http.Request) {
	input := r.URL.Query().Get("input")
	if !ValidateInput(input) {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}
	fmt.Fprintf(w, "Hello, %s!", input)
}

func main() {
	// Load configuration
	appName, err := loadConfig()
	if err != nil {
		log.Fatalf("Error: %v", err)
	}
	log.Printf("Starting application: %s", appName)

	// Set up HTTP server
	http.HandleFunc("/", handler)
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}