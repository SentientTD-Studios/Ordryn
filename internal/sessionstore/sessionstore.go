package sessionstore

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
)

var Store *sessions.CookieStore

const testSessionKey = "test-session-key-for-unit-tests-32chars!!"

func runningGoTest() bool {
	if strings.HasSuffix(os.Args[0], ".test") {
		return true
	}
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "-test.") {
			return true
		}
	}
	return false
}

func init() {
	// Load .env file
	err := godotenv.Load()
	if err != nil {
		log.Printf("Warning: Error loading .env file: %v\n", err)
	}

	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		if runningGoTest() {
			sessionKey = testSessionKey
		} else {
			log.Fatal("SESSION_KEY environment variable is not set")
		}
	}
	if len(sessionKey) < 32 {
		log.Fatal("SESSION_KEY must be at least 32 characters long")
	}

	Store = sessions.NewCookieStore([]byte(sessionKey))

	const sessionMaxAge = 86400 * 30 // 30 days
	Store.MaxAge(sessionMaxAge)

	Store.Options = &sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionMaxAge,
	}
}

func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	if Store == nil {
		return
	}
	sess, err := Store.Get(r, "session")
	if err != nil || sess == nil {
		// nothing to clear
		return
	}
	sess.Options = &sessions.Options{
		Path:   "/",
		MaxAge: -1,
	}
	_ = sess.Save(r, w)
}
