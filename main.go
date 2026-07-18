package main

import (
	"GoTodo/internal/server"
	"GoTodo/internal/storage"
	"fmt"
	"os"
)

func main() {
	fmt.Println("Application main function started.")
	storage.CreateDatabase()
	if err := storage.RunMigrations(); err != nil {
		fmt.Printf("Warning: migrations completed with errors: %v\n", err)
	}

	err := server.StartServer()
	if err != nil {
		fmt.Printf("Server error: %v\n", err)
		os.Exit(1)
	}
}
