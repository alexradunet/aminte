// Package google speaks to Google for the extension: OAuth for a Desktop client and the Photos Picker API.
// It uses the standard library only.
package google

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Scope is the one scope the extension asks for. It reads the items that the owner picks, and nothing else.
const Scope = "https://www.googleapis.com/auth/photospicker.mediaitems.readonly"

// Default endpoints of Google. A test replaces them in Config.
const (
	DefaultAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	DefaultTokenURL = "https://oauth2.googleapis.com/token"
)

const maxJSON = 4 << 20 // the largest JSON body read from Google

// ErrNotConnected means that the owner must sign in again: there is no token, or Google refused the refresh token.
// A refresh token of an app in "Testing" status expires after 7 days.
var ErrNotConnected = errors.New("not connected to Google: sign in again")

// Config is the OAuth client and its endpoints.
type Config struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
}

// LoadCredentials reads the JSON that Google Cloud gives for a Desktop client: {"installed": {...}}.
// The endpoints of Google are fixed. The file is not trusted to name another host for the secret.
func LoadCredentials(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read credentials: %w", err)
	}
	var f struct {
		Installed struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return Config{}, fmt.Errorf("parse credentials %s: %w", path, err)
	}
	in := f.Installed
	if in.ClientID == "" || in.ClientSecret == "" {
		return Config{}, fmt.Errorf("credentials %s: need an \"installed\" client with client_id and client_secret (a Desktop client)", path)
	}
	return Config{ClientID: in.ClientID, ClientSecret: in.ClientSecret, AuthURL: DefaultAuthURL, TokenURL: DefaultTokenURL}, nil
}

// Token is what Google gave and what the token file keeps.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// Auth owns the OAuth token. The access token is refreshed when it is about to expire.
type Auth struct {
	Config    Config
	HTTP      *http.Client
	TokenPath string           // the token file; see DefaultTokenPath
	Now       func() time.Time // nil means time.Now

	mu     sync.Mutex
	tok    Token
	loaded bool
}

// DefaultTokenPath is os.UserConfigDir()/aminte-gphotos/token.json.
func DefaultTokenPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("find the config folder: %w", err)
	}
	return filepath.Join(dir, "aminte-gphotos", "token.json"), nil
}

func (a *Auth) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// PKCEChallenge is the S256 challenge of a verifier (RFC 7636).
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// RandomString gives n random bytes as URL-safe text: a PKCE verifier (n = 32) or a state (n = 16).
func RandomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// AuthURL is the address that the owner opens to sign in.
func (a *Auth) AuthURL(redirect, state, verifier string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {a.Config.ClientID},
		"redirect_uri":          {redirect},
		"scope":                 {Scope},
		"state":                 {state},
		"code_challenge":        {PKCEChallenge(verifier)},
		"code_challenge_method": {"S256"},
		"access_type":           {"offline"},
		"prompt":                {"consent"},
	}
	return a.Config.AuthURL + "?" + q.Encode()
}

// Exchange trades the code of the callback for a token, and keeps the token in the file.
func (a *Auth) Exchange(ctx context.Context, code, verifier, redirect string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	tok, err := a.post(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirect},
	}, "")
	if err != nil {
		return err
	}
	if tok.RefreshToken == "" {
		return errors.New("no refresh token from Google")
	}
	return a.keep(tok)
}

// Connected tells whether a refresh token is held. It does not call Google.
func (a *Auth) Connected() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.load() == nil && a.tok.RefreshToken != ""
}

// AccessToken gives a valid access token, and refreshes it first when it expires within a minute.
func (a *Auth) AccessToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.load(); err != nil {
		return "", err
	}
	if a.tok.RefreshToken == "" {
		return "", ErrNotConnected
	}
	if a.tok.AccessToken != "" && a.now().Add(time.Minute).Before(a.tok.Expiry) {
		return a.tok.AccessToken, nil
	}
	tok, err := a.post(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {a.tok.RefreshToken}}, a.tok.RefreshToken)
	if err != nil {
		return "", err
	}
	if err := a.keep(tok); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// load reads the token file once. A missing file is not an error: the owner has not connected yet.
func (a *Auth) load() error {
	if a.loaded {
		return nil
	}
	b, err := os.ReadFile(a.TokenPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		a.loaded = true
		return nil
	case err != nil:
		return fmt.Errorf("read token file: %w", err)
	}
	if err := json.Unmarshal(b, &a.tok); err != nil {
		return fmt.Errorf("parse token file %s: %w", a.TokenPath, err)
	}
	a.loaded = true
	return nil
}

// post calls the token endpoint. A refresh answer may carry no refresh token: the old one stays (oldRefresh).
func (a *Auth) post(ctx context.Context, form url.Values, oldRefresh string) (Token, error) {
	form.Set("client_id", a.Config.ClientID)
	form.Set("client_secret", a.Config.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Config.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, fmt.Errorf("token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body, maxJSON)
	if err != nil {
		return Token{}, fmt.Errorf("token response: %w", err)
	}
	var out struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &out); err != nil && resp.StatusCode == http.StatusOK {
		return Token{}, fmt.Errorf("token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if oldRefresh != "" && out.Error == "invalid_grant" {
			return Token{}, fmt.Errorf("%w (%s)", ErrNotConnected, out.Description)
		}
		return Token{}, fmt.Errorf("token endpoint: status %d: %s %s", resp.StatusCode, out.Error, out.Description)
	}
	if out.AccessToken == "" {
		return Token{}, errors.New("token endpoint: no access token")
	}
	tok := Token{AccessToken: out.AccessToken, RefreshToken: out.RefreshToken, Expiry: a.now().Add(time.Duration(out.ExpiresIn) * time.Second)}
	if tok.RefreshToken == "" {
		tok.RefreshToken = oldRefresh
	}
	return tok, nil
}

// keep writes the token file with mode 0600: a temp file in the same folder, then a rename.
func (a *Auth) keep(tok Token) error {
	b, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("encode token: %w", err)
	}
	dir := filepath.Dir(a.TokenPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("make token folder: %w", err)
	}
	f, err := os.CreateTemp(dir, "token-*.tmp")
	if err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	defer os.Remove(f.Name()) // no effect after the rename
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return fmt.Errorf("write token file: %w", err)
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return fmt.Errorf("write token file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	if err := os.Rename(f.Name(), a.TokenPath); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	a.tok, a.loaded = tok, true
	return nil
}

// readLimited reads all of r, and fails when it holds more than max bytes.
func readLimited(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("body is larger than %d bytes", max)
	}
	return b, nil
}
