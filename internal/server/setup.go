package server

import (
	"fmt"
	"net"
	"os"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// announceSetup prints the first-time setup token while the site has no accounts.
// Whoever completes setup becomes the admin, so the token keeps a freshly started,
// publicly reachable server from being claimed by someone without access to its log.
func announceSetup(mode string) {
	// A fresh database has no settings row yet, and GET /api/v2/site needs one.
	if err := storage.EnsureSiteSettingsRow(); err != nil {
		fmt.Printf("Warning: could not create default site settings: %v\n", err)
	}
	token, err := domain.IssueSetupToken()
	if err != nil {
		fmt.Printf("Warning: could not check first-time setup: %v\n", err)
		return
	}
	if token == "" {
		return
	}
	fmt.Println("========================================================")
	fmt.Println("First-time setup required: this site has no accounts yet.")
	if mode == utils.ModeFull {
		fmt.Printf("Open %s in a browser and enter the setup token below.\n", setupURL())
	} else {
		fmt.Println("POST the setup form to /api/v2/setup with the setup token below (see openapi.yaml).")
	}
	fmt.Printf("Setup token: %s\n", token)
	fmt.Println("The token changes on every restart until setup is finished.")
	fmt.Println("========================================================")
}

// setupURL is the best local guess at the setup page; behind a proxy, use the public host instead.
func setupURL() string {
	host := os.Getenv("HOST")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	base := strings.TrimSuffix(utils.GetBasePath(), "/")
	if strings.Contains(base, "://") {
		return base + "/setup"
	}
	return "http://" + net.JoinHostPort(host, port) + base + "/setup"
}
