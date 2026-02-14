package gmail

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	gmailv1 "google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// NewService(ctx, configDir) initializes an OAuth-backed Gmail service using:
// - Client credentials at ~/.config/chuckterm/client_secret.json
// - Token cache at ~/.config/chuckterm/token.json
// Scopes: gmail.readonly and gmail.modify (for trash/untrash).
// NewService is a convenience wrapper for non-interactive authentication.
func NewService(ctx context.Context, configDir string) (*gmailv1.Service, error) {
	return NewServiceInteractive(ctx, configDir, nil, nil)
}

// NewServiceInteractive initializes a Gmail service, using the provided channels
// for interactive authentication if needed.
func NewServiceInteractive(ctx context.Context, configDir string, uiEvents chan<- interface{}, userResponses <-chan string) (*gmailv1.Service, error) {
	credPath := filepath.Join(configDir, "client_secret.json")
	b, err := os.ReadFile(credPath)
	if err != nil {
		return nil, fmt.Errorf("read credentials at %s: %w", credPath, err)
	}

	cfg, err := google.ConfigFromJSON(b,
		gmailv1.GmailReadonlyScope,
		gmailv1.GmailModifyScope,
	)
	if err != nil {
		return nil, fmt.Errorf("parse oauth config: %w", err)
	}

	tokFile := filepath.Join(configDir, "token.json")
	tok, err := readToken(tokFile)
	if err == nil {
		// Validate the cached token by making a lightweight API call.
		client := cfg.Client(ctx, tok)
		svc, err := gmailv1.NewService(ctx, option.WithHTTPClient(client))
		if err == nil {
			_, err = svc.Users.GetProfile("me").Do()
		}
		if err == nil {
			return svc, nil
		}
		// Token is invalid/expired — remove it and fall through to re-auth.
		os.Remove(tokFile)
	}

	// Do interactive auth flow.
	tok, err = getTokenFromWeb(ctx, cfg, uiEvents, userResponses)
	if err != nil {
		return nil, err
	}
	if err := saveToken(tokFile, tok); err != nil {
		return nil, err
	}

	client := cfg.Client(ctx, tok)
	svc, err := gmailv1.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("create gmail service: %w", err)
	}
	return svc, nil
}

func readToken(path string) (*oauth2.Token, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var tok oauth2.Token
	if err := json.NewDecoder(f).Decode(&tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func saveToken(path string, tok *oauth2.Token) error {
	tmp := path + ".tmp"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(tok); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

// getTokenFromWeb runs a loopback HTTP server to capture the auth code.
// If that fails or times out, it falls back to manual paste (code or URL).
func getTokenFromWeb(ctx context.Context, cfg *oauth2.Config, uiEvents chan<- interface{}, userResponses <-chan string) (*oauth2.Token, error) {
	// If we have channels, use the interactive flow.
	if uiEvents != nil && userResponses != nil {
		return getTokenFromWebInteractive(ctx, cfg, uiEvents, userResponses)
	}
	return getTokenFromWebCLI(ctx, cfg)
}

// getTokenFromWebInteractive handles the web-based auth flow using a loopback
// HTTP server to capture the redirect. Falls back to manual code paste via the
// TUI channels if the loopback fails.
func getTokenFromWebInteractive(ctx context.Context, cfg *oauth2.Config, uiEvents chan<- interface{}, userResponses <-chan string) (*oauth2.Token, error) {
	// Start a loopback server to capture the OAuth redirect.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen on loopback: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirect := fmt.Sprintf("http://127.0.0.1:%d/", port)

	oldRedirect := cfg.RedirectURL
	cfg.RedirectURL = redirect
	defer func() { cfg.RedirectURL = oldRedirect }()

	type result struct {
		code string
		err  error
	}
	resCh := make(chan result, 1)

	mux := http.NewServeMux()
	srv := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		Handler:           mux,
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "Missing 'code' parameter", http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "Authentication complete. You can close this window.")
		select {
		case resCh <- result{code: code}:
		default:
		}
		go func() { _ = srv.Shutdown(context.Background()) }()
	})
	go func() { _ = srv.Serve(ln) }()

	authURL := cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	uiEvents <- authURL

	// Wait for the loopback redirect, manual paste, or cancellation.
	select {
	case <-ctx.Done():
		_ = srv.Shutdown(context.Background())
		return nil, ctx.Err()
	case r := <-resCh:
		if r.err != nil {
			return nil, r.err
		}
		tok, err := cfg.Exchange(ctx, strings.TrimSpace(r.code))
		if err != nil {
			return nil, fmt.Errorf("token exchange: %w", err)
		}
		return tok, nil
	case input := <-userResponses:
		_ = srv.Shutdown(context.Background())
		if input == "" {
			return nil, errors.New("empty authorization code")
		}
		code := input
		if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
			u, err := url.Parse(input)
			if err != nil {
				return nil, fmt.Errorf("parse redirect URL: %w", err)
			}
			c := u.Query().Get("code")
			if c == "" {
				return nil, errors.New("no 'code' parameter found in pasted URL")
			}
			code = c
		}
		tok, err := cfg.Exchange(ctx, strings.TrimSpace(code))
		if err != nil {
			return nil, fmt.Errorf("token exchange: %w", err)
		}
		return tok, nil
	}
}

// getTokenFromWebCLI handles the original CLI-based auth flow.
func getTokenFromWebCLI(ctx context.Context, cfg *oauth2.Config) (*oauth2.Token, error) {
	// Try loopback on a random localhost port.
	type result struct {
		code string
		err  error
	}
	resCh := make(chan result, 1)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err == nil {
		port := ln.Addr().(*net.TCPAddr).Port
		redirect := fmt.Sprintf("http://127.0.0.1:%d/", port)
		oldRedirect := cfg.RedirectURL
		cfg.RedirectURL = redirect

		mux := http.NewServeMux()
		srv := &http.Server{
			ReadHeaderTimeout: 5 * time.Second,
			Handler:           mux,
		}
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			code := r.URL.Query().Get("code")
			if code == "" {
				http.Error(w, "Missing 'code' parameter", http.StatusBadRequest)
				return
			}
			fmt.Fprintln(w, "Authentication complete. You can close this window.")
			select {
			case resCh <- result{code: code}:
			default:
			}
			go func() { _ = srv.Shutdown(context.Background()) }()
		})
		go func() { _ = srv.Serve(ln) }()

		authURL := cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
		fmt.Fprintln(os.Stderr, "A browser window will open. If it does not, copy this URL:")
		fmt.Fprintln(os.Stderr, authURL)
		fmt.Fprintf(os.Stderr, "Waiting for redirect on %s …\n", redirect)

		// Wait for code or timeout, while honoring ctx.
		select {
		case <-ctx.Done():
			cfg.RedirectURL = oldRedirect
			return nil, ctx.Err()
		case r := <-resCh:
			if r.err != nil {
				return nil, r.err
			}
			fmt.Fprintln(os.Stderr, "Exchanging code for token…")
			tok, err := cfg.Exchange(ctx, strings.TrimSpace(r.code))
			if err != nil {
				return nil, fmt.Errorf("token exchange: %w", err)
			}
			fmt.Fprintln(os.Stderr, "Authentication successful.")
			// Restore redirect only after successful exchange to avoid invalid_grant.
			cfg.RedirectURL = oldRedirect
			return tok, nil
		case <-time.After(120 * time.Second):
			// Fall through to manual paste.
			cfg.RedirectURL = oldRedirect
			fmt.Fprintln(os.Stderr, "Timeout waiting for redirect; falling back to manual paste.")
		}
	}

	// Manual paste fallback.
	authURL := cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintln(os.Stderr, "Open this URL in your browser to authorize Chuckterm:")
	fmt.Fprintln(os.Stderr, authURL)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Paste the AUTH CODE itself or the FULL redirect URL here, then press Enter.")
	fmt.Fprint(os.Stderr, "> ")

	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 1024), 1024*1024)
	if !sc.Scan() {
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("read auth code: %w", err)
		}
		return nil, errors.New("empty authorization code")
	}
	input := strings.TrimSpace(sc.Text())
	if input == "" {
		return nil, errors.New("empty authorization code")
	}

	code := input
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		u, err := url.Parse(input)
		if err != nil {
			return nil, fmt.Errorf("parse redirect URL: %w", err)
		}
		c := u.Query().Get("code")
		if c == "" {
			return nil, errors.New("no 'code' parameter found in pasted URL")
		}
		code = c
	}

	fmt.Fprintln(os.Stderr, "Exchanging code for token…")
	tok, err := cfg.Exchange(ctx, strings.TrimSpace(code))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	fmt.Fprintln(os.Stderr, "Authentication successful.")
	return tok, nil
}