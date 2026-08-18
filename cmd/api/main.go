package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"thundercall-go/internal/config"
	"thundercall-go/internal/database"
	"thundercall-go/internal/geocode"
	"thundercall-go/internal/httpapi"
	"thundercall-go/internal/models"
	accountsrepo "thundercall-go/internal/repositories/accounts"
	apiusersrepo "thundercall-go/internal/repositories/apiusers"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	switch command := currentCommand(); command {
	case "create-user":
		if err := runCreateUser(cfg, os.Args[2:]); err != nil {
			log.Fatalf("create user: %v", err)
		}
	case "healthcheck":
		if err := runHealthcheck(cfg); err != nil {
			log.Fatalf("api healthcheck: %v", err)
		}
	default:
		if err := runServer(cfg); err != nil {
			log.Fatalf("run api server: %v", err)
		}
	}
}

func currentCommand() string {
	if len(os.Args) >= 2 && !strings.HasPrefix(os.Args[1], "-") {
		return strings.TrimSpace(os.Args[1])
	}
	return "serve"
}

func runServer(cfg config.Config) error {
	if !cfg.MySQL.Enabled() {
		return fmt.Errorf("THUNDERCALL_MYSQL_DSN is required")
	}

	db, err := database.OpenMySQL(context.Background(), cfg.MySQL)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()

	server := &http.Server{
		Addr:              cfg.API.ListenAddr,
		Handler:           httpapi.NewServer(db, cfg.API.SessionTTL, geocode.New(cfg.Geocoding)).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("thundercall api listening on %s", cfg.API.ListenAddr)
	return server.ListenAndServe()
}

func runHealthcheck(cfg config.Config) error {
	url, err := apiHealthzURL(cfg.API.ListenAddr)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build /healthz request: %w", err)
	}

	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("call %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("unexpected /healthz status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

func apiHealthzURL(listenAddr string) (string, error) {
	addr := strings.TrimSpace(listenAddr)
	if addr == "" {
		addr = ":8080"
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("parse THUNDERCALL_API_LISTEN_ADDR %q: %w", listenAddr, err)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}

	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}

func runCreateUser(cfg config.Config, args []string) error {
	if !cfg.MySQL.Enabled() {
		return fmt.Errorf("THUNDERCALL_MYSQL_DSN is required")
	}

	fs := flag.NewFlagSet("create-user", flag.ContinueOnError)
	accountID := fs.Int64("account-id", 0, "Account ID for the API user")
	email := fs.String("email", "", "Login email")
	password := fs.String("password", "", "Login password")
	displayName := fs.String("display-name", "", "Optional display name")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *accountID <= 0 {
		return fmt.Errorf("--account-id is required")
	}
	if strings.TrimSpace(*email) == "" {
		return fmt.Errorf("--email is required")
	}
	if strings.TrimSpace(*password) == "" {
		return fmt.Errorf("--password is required")
	}

	db, err := database.OpenMySQL(context.Background(), cfg.MySQL)
	if err != nil {
		return fmt.Errorf("open mysql: %w", err)
	}
	defer db.Close()

	accounts := accountsrepo.New(db)
	account, err := accounts.GetByID(context.Background(), *accountID)
	if err != nil {
		return fmt.Errorf("lookup account %d: %w", *accountID, err)
	}
	if account == nil {
		return fmt.Errorf("account %d not found", *accountID)
	}

	passwordHash, err := httpapi.HashPassword(*password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	var displayNamePtr *string
	if value := strings.TrimSpace(*displayName); value != "" {
		displayNamePtr = &value
	}

	user := &models.APIUser{
		AccountID:    *accountID,
		Email:        strings.TrimSpace(*email),
		PasswordHash: passwordHash,
		DisplayName:  displayNamePtr,
		Active:       true,
	}

	if err := apiusersrepo.New(db).Create(context.Background(), user); err != nil {
		return fmt.Errorf("insert api user: %w", err)
	}

	log.Printf("created api user %d for account %d (%s)", user.ID, user.AccountID, user.Email)
	return nil
}
