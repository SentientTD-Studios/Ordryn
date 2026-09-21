package handlers

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	"GoTodo/internal/domain"
	"GoTodo/internal/imagehost"
	"GoTodo/internal/server/utils"
)

// APIV1MeAvatar handles POST/DELETE /api/v2/me/avatar.
func APIV1MeAvatar(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		apiV1PostMeAvatar(w, r)
	case http.MethodDelete:
		apiV1DeleteMeAvatar(w, r)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1PostMeAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	cfg, err := loadImageHosting()
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load image hosting settings.")
		return
	}
	if !cfg.Enabled() {
		utils.APIJSONError(w, http.StatusBadRequest, "not_configured", "Image hosting is not configured.")
		return
	}

	max := imagehost.ClampMaxBytes(cfg.MaxBytes)
	if err := r.ParseMultipartForm(max + 64*1024); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid form or file too large.")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Missing file upload (field name: file).")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, max+1))
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Failed to read file.")
		return
	}
	if int64(len(data)) > max {
		utils.APIJSONError(w, http.StatusRequestEntityTooLarge, "too_large",
			fmt.Sprintf("Image exceeds the %d byte limit.", max))
		return
	}
	if len(data) == 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "File is empty.")
		return
	}

	ct, err := imagehost.DetectImage(data)
	if err != nil || (ct != imagehost.TypePNG && ct != imagehost.TypeJPEG) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Profile picture must be a PNG or JPEG image.")
		return
	}

	key, err := imagehost.NewObjectKey(ct)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to generate object key.")
		return
	}
	obj := imagehost.Object{Key: key, ContentType: ct, Data: data}

	if imagehost.NormalizeProvider(cfg.Provider) == imagehost.ProviderLocal {
		cfg.LocalPublicBase = localPublicBase(r)
	}

	store, err := openImageStore(cfg)
	if err != nil {
		if errors.Is(err, imagehost.ErrDisabled) {
			utils.APIJSONError(w, http.StatusBadRequest, "not_configured", "Image hosting is not configured.")
			return
		}
		log.Printf("avatar image store open failed: %v", err)
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Image hosting is not configured correctly.")
		return
	}

	url, err := store.Put(r.Context(), obj)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	profile, err := domain.UpdateAvatarURL(r.Context(), userID, url)
	if err != nil {
		log.Printf("failed to update user avatar: %v", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update profile picture.")
		return
	}

	refreshSessionProfile(w, r, profile)
	writeAPIUserJSON(w, http.StatusOK, profile)
}

func apiV1DeleteMeAvatar(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	profile, err := domain.RemoveAvatar(r.Context(), userID)
	if err != nil {
		log.Printf("failed to remove avatar: %v", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to remove profile picture.")
		return
	}

	refreshSessionProfile(w, r, profile)
	writeAPIUserJSON(w, http.StatusOK, profile)
}
