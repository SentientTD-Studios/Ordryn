package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"GoTodo/internal/imagehost"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

const uploadsPathPrefix = "/uploads/"

// loadImageHosting is overridable in tests.
var loadImageHosting = defaultLoadImageHosting

func defaultLoadImageHosting() (imagehost.Config, error) {
	s, err := storage.GetSiteSettings()
	if err != nil || s == nil {
		return imagehost.Config{}, fmt.Errorf("load site settings")
	}
	return s.ImageHostingConfig()
}

// openImageStore is overridable in tests.
var openImageStore = imagehost.NewStore

// APIV1Images handles POST /api/v2/images (multipart field "file").
func APIV1Images(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if _, ok := utils.GetAPIUserID(r); !ok {
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
	// Cap the multipart parse a bit above the image limit so the boundary still fits.
	if err := r.ParseMultipartForm(max + 64*1024); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid form or file too large.")
		return
	}
	file, header, err := r.FormFile("file")
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
	obj, err := imagehost.Prepare(cfg, data)
	if err != nil {
		writeImageUploadError(w, err, max)
		return
	}

	if imagehost.NormalizeProvider(cfg.Provider) == imagehost.ProviderLocal {
		cfg.LocalPublicBase = localPublicBase(r)
	}

	store, err := openImageStore(cfg)
	if err != nil {
		if errors.Is(err, imagehost.ErrDisabled) {
			utils.APIJSONError(w, http.StatusBadRequest, "not_configured", "Image hosting is not configured.")
			return
		}
		log.Printf("image store open failed: %v", err)
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Image hosting is not configured correctly.")
		return
	}

	url, err := store.Put(r.Context(), obj)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	name := ""
	if header != nil {
		name = header.Filename
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"url":          url,
		"content_type": obj.ContentType,
		"size":         len(obj.Data),
		"filename":     name,
		"key":          obj.Key,
	})
}

func writeStoreError(w http.ResponseWriter, err error) {
	var ue *imagehost.UploadError
	if errors.As(err, &ue) {
		log.Printf("image upload failed: %s", ue.Error())
		utils.APIJSONError(w, http.StatusBadGateway, "upload_failed", ue.UserMessage())
		return
	}
	log.Printf("image upload failed: %v", err)
	utils.APIJSONError(w, http.StatusBadGateway, "upload_failed", "Couldn't upload that image. Try again later.")
}

func writeImageUploadError(w http.ResponseWriter, err error, max int64) {
	switch {
	case errors.Is(err, imagehost.ErrDisabled):
		utils.APIJSONError(w, http.StatusBadRequest, "not_configured", "Image hosting is not configured.")
	case errors.Is(err, imagehost.ErrNotImage):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "File is not a supported image (jpeg, png, gif, or webp).")
	case strings.Contains(err.Error(), "exceeds"):
		utils.APIJSONError(w, http.StatusRequestEntityTooLarge, "too_large",
			fmt.Sprintf("Image exceeds the %d byte limit.", max))
	default:
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Couldn't upload that image.")
	}
}

func localPublicBase(r *http.Request) string {
	return strings.TrimSuffix(utils.AbsoluteURLForRequest(r, "/uploads"), "/")
}

// ServeLocalImage serves GET /uploads/{key} from the configured local directory.
func ServeLocalImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	key := localUploadKey(r.URL.Path)
	if !imagehost.SafeObjectKey(key) {
		http.NotFound(w, r)
		return
	}
	cfg, err := loadImageHosting()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dir := cfg.LocalPath
	if dir == "" {
		dir = imagehost.DefaultLocalPath
	}
	store, err := imagehost.NewLocalStore(dir, "")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	data, contentType, err := store.Read(key)
	if err != nil {
		if os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Failed to read image.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func localUploadKey(path string) string {
	path = utils.TrimPublicPrefix(path)
	path = strings.TrimPrefix(path, uploadsPathPrefix)
	path = strings.TrimPrefix(path, "/")
	return path
}
