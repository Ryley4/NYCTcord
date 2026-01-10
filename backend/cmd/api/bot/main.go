package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Ryley4/NYCTcord/backend/internal/db"
	"github.com/bwmarrin/discordgo"
)

type PendingNotification struct {
	ID        int64
	DiscordID string
	LineID    string
	Header    sql.NullString
	Body      sql.NullString
	Status    sql.NullString
	Effect    sql.NullString
	CreatedAt string
}

func main() {
	token := strings.TrimSpace(os.Getenv(""))
	if token == "" {
		log.Fatal("DISCORD_BOT_TOKEN is not set")
	}

	database, err := db.Open("nyctcord.db")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	dg, err := discordgo.New("Bot " + token)
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

	processOnce(ctx, database, dg)

	for {
		select {
		case <-ctx.Done():
			log.Println("bot: shutting down")
			return
		case <-ticker.C:
			processOnce(ctx, database, dg)
		}
	}
}

func processOnce(ctx context.Context, database *db.DB, dg *discordgo.Session) {
	pending, err := loadPending(database, 25)
	if err != nil {
		log.Printf("bot: load pending error: %v", err)
		return
	}
	if len(pending) == 0 {
		return
	}

	for _, n := range pending {
		if err := sendDMEmbed(dg, n.DiscordID, buildEmbed(n)); err != nil {
			log.Printf("bot: send failed notif_id=%d discord_id=%s err=%v", n.ID, n.DiscordID, err)
			_ = markFailed(database, n.ID, err.Error())
			continue
		}
		_ = markSent(database, n.ID)
	}
}

func loadPending(database *db.DB, limit int) ([]PendingNotification, error) {
	rows, err := database.Query(`
		SELECT
			n.id,
			u.discord_id,
			n.line_id,
			a.header,
			a.body,
			a.effect,
			n.created_at
		FROM notifications n
		JOIN users u ON u.id = n.user_id
		JOIN alerts a ON a.id = n.alert_id
		WHERE n.status = 'pending'
		ORDER BY n.id
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]PendingNotification, 0)
	for rows.Next() {
		var p PendingNotification
		if err := rows.Scan(&p.ID, &p.DiscordID, &p.LineID, &p.Header, &p.Body, &p.Effect, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func sendDMEmbed(dg *discordgo.Session, discordUserID string, embed *discordgo.MessageEmbed) error {
	ch, err := dg.UserChannelCreate(discordUserID)
	if err != nil {
		return err
	}
	_, err = dg.ChannelMessageSendEmbed(ch.ID, embed)
	return err
}

func buildEmbed(n PendingNotification) *discordgo.MessageEmbed {
	title := "Service update"
	if n.Header.Valid && strings.TrimSpace(n.Header.String) != "" {
		title = n.Header.String
	}

	desc := "Check service status for details."
	if n.Body.Valid && strings.TrimSpace(n.Body.String) != "" {
		desc = truncate(strings.TrimSpace(n.Body.String), 3500)
	}

	line := strings.ToUpper(strings.TrimSpace(n.LineID))
	color := lineColorBrandExact(line)

	footer := fmt.Sprintf("nyctcord • Line %s", line)
	if line == "" {
		footer = "nyctcord"
	}

	embed := &discordgo.MessageEmbed{
		Title:       title,
		Description: desc,
		Color:       color,
		Footer:      &discordgo.MessageEmbedFooter{Text: footer},
	}

	if n.Effect.Valid && strings.TrimSpace(n.Effect.String) != "" {
		embed.Fields = []*discordgo.MessageEmbedField{
			{Name: "Effect", Value: strings.TrimSpace(n.Effect.String), Inline: true},
		}
	}

	return embed
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func lineColorBrandExact(line string) int {
	// Hex -> int (0xRRGGBB)
	const (
		blue     = 0x0062CF // A/C/E
		orange   = 0xEB6800 // B/D/F/M
		lightGrn = 0x799534 // G
		brown    = 0x8E5C33 // J/Z
		grey     = 0x7C858C // L + Shuttle S
		yellow   = 0xF6BC26 // N/Q/R/W
		red      = 0xD82233 // 1/2/3
		darkGrn  = 0x009952 // 4/5/6
		purple   = 0x9A38A1 // 7
		teal     = 0x008EB7 // T
		mtaBlue  = 0x08179C // SIR
		fallback = 0x08179C // default to MTA Blue
	)

	switch line {
	case "A", "C", "E":
		return blue

	case "B", "D", "F", "M":
		return orange

	case "G":
		return lightGrn

	case "J", "Z":
		return brown

	case "L", "S", "GS", "FS", "H":
		return grey

	case "N", "Q", "R", "W":
		return yellow

	case "1", "2", "3":
		return red

	case "4", "5", "6":
		return darkGrn

	case "7":
		return purple

	case "T":
		return teal

	case "SI", "SIR":
		return mtaBlue

	case "ALL":
		return fallback

	default:
		return fallback
	}
}

func markSent(database *db.DB, id int64) error {
	_, err := database.Exec(`
		UPDATE notifications
		SET status='sent', sent_at=datetime('now'), last_error=NULL
		WHERE id=?
	`, id)
	return err
}

func markFailed(database *db.DB, id int64, msg string) error {
	msg = strings.TrimSpace(msg)
	if len(msg) > 400 {
		msg = msg[:400]
	}
	_, err := database.Exec(`
		UPDATE notifications
		SET status='failed', last_error=?, sent_at=NULL
		WHERE id=?
	`, msg, id)
	return err
}
