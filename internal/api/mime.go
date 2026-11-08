package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"aminte/internal/core"
)

// The type of a file the owner keeps, named from its extension or its first bytes: what the api says of an original
// that a browser or the CLI sends, before core keeps it.

// extTypes names the files the owner keeps by their extension: fixed here, not the operating system's table, so every
// machine names a file alike.
var extTypes = map[string]string{
	".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
	".heic": "image/heic", ".heif": "image/heif", ".avif": "image/avif", ".tif": "image/tiff", ".tiff": "image/tiff",
	".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm", ".mkv": "video/x-matroska",
	".3gp": "video/3gpp", ".m4a": "audio/mp4", ".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg",
	".opus": "audio/ogg", ".flac": "audio/flac", ".aac": "audio/aac", ".amr": "audio/amr", ".pdf": "application/pdf",
	".txt": "text/plain", ".md": "text/markdown", ".html": "text/html", ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
}

// mimeOf names a file's type from its name, else from its first bytes (net/http's sniffing).
func mimeOf(name string, head []byte) string {
	if t, ok := extTypes[strings.ToLower(filepath.Ext(name))]; ok {
		return t
	}
	if t := core.NormalMIME(http.DetectContentType(head)); t != "" {
		return t
	}
	return "application/octet-stream"
}
