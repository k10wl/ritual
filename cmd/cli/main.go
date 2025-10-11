package main

import (
	"fmt"
	"log"

	"github.com/joho/godotenv"
)

func main() {
	// Load .env file from project root
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: .env file not found: %v", err)
	}

	fmt.Println("Hello, World!")
}
