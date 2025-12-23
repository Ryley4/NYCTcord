package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Ryley4/NYCTcord/backend/internal/db"
)

type PendingNotification struct {
	ID        int64
	DiscordID string
	LineID    string
	Header    sql.NullString
	Body      sql.NullString
	Effect    sql.NullString
	CreatedAt string
}

func main() {
	token := strings.TrimSpace(os.Getenv("TOKEN"))
	if token == "" {
		log.fatal("Token is not set.")
	}

	database, err := db.Open("nyctcord.db")
	if err != nil {
		log.Fatalf("discord init: %v", err)
	}
	dg.Identify.Intents = 0

	if err := dg.Open(); err != nil {
		log.Fatalf("discord open: %v", err)
	}
	defer dg.Close()

	log.Println("bot: connected")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

}
