package tenant

import (
	"net/http"

	"github.com/google/uuid"
)

// OSSMiddleware injects the default OSS customer into every request. It is
// the OSS equivalent of cloud's session-based tenant resolution. Mount this
// high in the chain so every downstream handler can rely on FromContext.
func OSSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := WithID(r.Context(), DefaultOSSID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Middleware returns a middleware that resolves the customer via the given
// resolver. Cloud wires its session/JWT resolver here. If the resolver
// returns a zero UUID, the request is rejected with 401.
func Middleware(resolve func(r *http.Request) (uuid.UUID, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := resolve(r)
			if !ok || id == uuid.Nil {
				http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
				return
			}
			ctx := WithID(r.Context(), id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
