package handlers

import "net/http"

// FavoriteDeprecationMessage is returned in Warning headers and JSON when a
// client uses the deprecated favorite field. Favoriting remains accepted until API v2.
const FavoriteDeprecationMessage = "Task favoriting is deprecated and will be removed in API v2."

const favoriteDeprecationWarning = `299 - "` + FavoriteDeprecationMessage + `"`

// setFavoriteDeprecationNotice marks a response as using the deprecated favorite field.
func setFavoriteDeprecationNotice(w http.ResponseWriter) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Warning", favoriteDeprecationWarning)
}

// favoriteDeprecationNoticeIfUsed sets Deprecation/Warning headers when used is true
// and returns the JSON deprecation_notice value (empty when the field was not used).
func favoriteDeprecationNoticeIfUsed(w http.ResponseWriter, used bool) string {
	if !used {
		return ""
	}
	setFavoriteDeprecationNotice(w)
	return FavoriteDeprecationMessage
}
