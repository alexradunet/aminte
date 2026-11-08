package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"aminte/internal/core"
	"aminte/siren"
)

// tokenMinLength is the shortest token that RequireToken accepts.
const tokenMinLength = 32

// CheckToken reports whether t may be the token of a server: one line of at least tokenMinLength characters, with no
// white space at its start or end. RequireToken cuts the white space of the credential, and a header value has none at
// its ends, so a client could not send such a token.
func CheckToken(t string) error {
	if len(t) < tokenMinLength {
		return fmt.Errorf("the token must be at least %d characters", tokenMinLength)
	}
	if strings.ContainsAny(t, "\r\n") {
		return errors.New("the token must be one line")
	}
	if strings.TrimSpace(t) != t {
		return errors.New("the token must not start or end with a space")
	}
	return nil
}

// RequireToken is next behind the owner's token: a request without the header "Authorization: Bearer TOKEN" gets
// 401 with the code token_required and "WWW-Authenticate: Bearer realm=\"aminte\"". The token is the only credential:
// there is no cookie, no login page and no exempt route. The compare is of two digests, so it takes the same time for
// every value, also for a value of another length. It refuses a token that CheckToken refuses.
//
// aminte serve puts it inside NetworkAuthority, so a request with a wrong Host gets 403 before it gets 401. The
// in-process client and the temporary server of `aminte NAME` do not use it.
func RequireToken(token string, next http.Handler) (http.Handler, error) {
	if err := CheckToken(token); err != nil {
		return nil, err
	}
	want := sha256.Sum256([]byte(token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, credential, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		got := sha256.Sum256([]byte(strings.TrimSpace(credential)))
		if ok && strings.EqualFold(scheme, "Bearer") && subtle.ConstantTimeCompare(got[:], want[:]) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="aminte"`)
		writeError(w, r, &core.Error{Status: http.StatusUnauthorized, Msg: "token required", Code: siren.CodeTokenRequired})
	}), nil
}
