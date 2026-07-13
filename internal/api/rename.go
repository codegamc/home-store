package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/codegamc/home-store/internal/s3"
	"github.com/codegamc/home-store/internal/storage"
)

// handleRenameObject implements S3's RenameObject request form. It is used by
// directory-bucket clients; path-style routing remains intentionally supported
// for Home Store's trusted-LAN deployment model.
func (h *Handler) handleRenameObject(w http.ResponseWriter, r *http.Request) {
	dstBucket, dstKey := parseBucketKey(r.URL.Path)
	rawSource := strings.TrimPrefix(r.Header.Get("x-amz-rename-source"), "/")
	source, err := url.PathUnescape(rawSource)
	if err != nil {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid x-amz-rename-source header")
		return
	}
	parts := strings.SplitN(source, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		h.errorResponse(w, http.StatusBadRequest, s3.InvalidRequest, "invalid x-amz-rename-source header")
		return
	}
	meta, err := h.backend.RenameObject(r.Context(), parts[0], parts[1], dstBucket, dstKey)
	if err != nil {
		switch err {
		case storage.ErrBucketNotFound:
			h.errorResponse(w, http.StatusNotFound, s3.NoSuchBucket, "bucket does not exist")
		case storage.ErrObjectNotFound:
			h.errorResponse(w, http.StatusNotFound, s3.NoSuchKey, "the specified key does not exist")
		default:
			h.errorResponse(w, http.StatusInternalServerError, s3.InternalError, "failed to rename object")
		}
		return
	}
	w.Header().Set("ETag", meta.ETag)
	w.WriteHeader(http.StatusOK)
}
