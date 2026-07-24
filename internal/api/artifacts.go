package api

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jesse0michael/evoke/internal/ent"
	"github.com/jesse0michael/evoke/internal/store"
	"github.com/jesse0michael/pkg/auth"
	httperrors "github.com/jesse0michael/pkg/http/errors"
	server "github.com/jesse0michael/pkg/http/server"
)

// maxArtifactBytes caps an uploaded .evoke document. These are small text
// files; the limit guards against abuse, not real content.
const maxArtifactBytes = 1 << 20 // 1 MiB

type versionView struct {
	Version   int       `json:"version"`
	SHA256    string    `json:"sha256"`
	CreatedAt time.Time `json:"created_at"`
}

func toVersionView(v *ent.Version) versionView {
	return versionView{Version: v.Version, SHA256: v.Sha256, CreatedAt: v.CreatedAt}
}

// push appends a new immutable version of {namespace}/{name} from the raw
// .evoke request body. The authenticated subject becomes the artifact owner on
// first push.
func (s *Server) push() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		subject, ok := auth.Subject(ctx)
		if !ok {
			httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusUnauthorized, "unauthenticated", ""))
			return
		}

		namespace := r.PathValue("namespace")
		name := r.PathValue("name")

		content, err := io.ReadAll(io.LimitReader(r.Body, maxArtifactBytes))
		if err != nil {
			httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusBadRequest, "failed to read body", err.Error()))
			return
		}
		if len(content) == 0 {
			httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusBadRequest, "empty artifact body", ""))
			return
		}

		sum := sha256.Sum256(content)
		digest := hex.EncodeToString(sum[:])

		v, err := s.store.PushVersion(ctx, subject, namespace, name, content, digest)
		if err != nil {
			httperrors.WriteError(ctx, w, err)
			return
		}

		_ = server.Encode(w, http.StatusCreated, toVersionView(v))
	}
}

// listVersions returns every published version of {namespace}/{name}.
func (s *Server) listVersions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	vs, err := s.store.Versions(ctx, r.PathValue("namespace"), r.PathValue("name"))
	if err != nil {
		httperrors.WriteError(ctx, w, err)
		return
	}
	if len(vs) == 0 {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusNotFound, "artifact not found", ""))
		return
	}

	views := make([]versionView, len(vs))
	for i, v := range vs {
		views[i] = toVersionView(v)
	}
	_ = server.Encode(w, http.StatusOK, map[string]any{"versions": views})
}

// pull returns the raw .evoke bytes of a specific version. The sha256 digest is
// returned in a header so clients can verify the download.
func (s *Server) pull(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ver, err := strconv.Atoi(r.PathValue("version"))
	if err != nil || ver < 1 {
		httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusBadRequest, "invalid version", ""))
		return
	}

	v, err := s.store.Version(ctx, r.PathValue("namespace"), r.PathValue("name"), ver)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httperrors.WriteError(ctx, w, httperrors.NewError(http.StatusNotFound, "version not found", ""))
			return
		}
		httperrors.WriteError(ctx, w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Evoke-SHA256", v.Sha256)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(v.Content)
}
