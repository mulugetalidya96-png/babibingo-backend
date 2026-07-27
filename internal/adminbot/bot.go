package adminbot

import (
	"context"
	"log"

	"babibingo/internal/config"

	"github.com/mymmrac/telego"
	"gorm.io/gorm"
)

type Bot struct {
    api      *telego.Bot
    me       *telego.User
    db       *gorm.DB
    cfg      *config.Config
    admins   []int64
    botName  string
}

func New(token string, db *gorm.DB, cfg *config.Config) (*Bot, error) {
    api, err := telego.NewBot(token)
    if err != nil {
        return nil, err
    }

    ctx := context.Background()
    me, err := api.GetMe(ctx)
    if err != nil {
        return nil, err
    }

    // Auto-migrate
    if err := db.AutoMigrate(&AgentRequest{}, &AdminActionLog{}, &AdminConfig{}); err != nil {
        log.Printf("Failed to migrate admin models: %v", err)
    }

    // Load admin IDs
    adminIDs := cfg.GetAdminIDs()
    if len(adminIDs) == 0 {
        log.Println("⚠️ WARNING: No admin IDs configured! Set ADMIN_IDS in environment")
    } else {
        log.Printf("✅ Admin IDs loaded: %v", adminIDs)
    }

    b := &Bot{
        api:    api,
        me:     me,
        db:     db,
        cfg:    cfg,
        admins: adminIDs,
        botName: me.Username,
    }

    // Set commands
    if err := b.setupCommands(ctx); err != nil {
        log.Printf("Failed to set commands: %v", err)
    }

    log.Printf("🤖 Admin Bot started: @%s", me.Username)
    log.Printf("🔐 Admin access: %d admins configured", len(adminIDs))

    return b, nil
}

func (b *Bot) setupCommands(ctx context.Context) error {
    commands := []telego.BotCommand{
        {Command: "start", Description: "Start admin bot"},
        {Command: "help", Description: "Show help menu"},
        {Command: "agents", Description: "Manage agents"},
        {Command: "deposits", Description: "Manage deposits"},
        {Command: "withdrawals", Description: "Manage withdrawals"},
        {Command: "games", Description: "Monitor games"},
        {Command: "bots", Description: "Manage game bots"},
        {Command: "users", Description: "Manage users"},
        {Command: "stats", Description: "View statistics"},
        {Command: "settings", Description: "Bot settings"},
    }

     err := b.api.SetMyCommands(ctx, &telego.SetMyCommandsParams{
        Commands: commands,
    })
    return err
}

func (b *Bot) Start(ctx context.Context) error {
    log.Printf("🚀 Starting admin bot @%s...", b.me.Username)

    var offset int = 0

    for {
        select {
        case <-ctx.Done():
            log.Println("Admin bot stopped")
            return nil
        default:
            updates, err := b.api.GetUpdates(ctx, &telego.GetUpdatesParams{
                Offset:  offset,
                Timeout: 60,
                Limit:   100,
            })
            if err != nil {
                log.Printf("Error getting updates: %v", err)
                continue
            }

            for _, update := range updates {
                if update.Message != nil {
                    b.handleMessage(ctx, update.Message)
                }
                if update.CallbackQuery != nil {
                    b.handleCallback(ctx, update.CallbackQuery)
                }
                offset = update.UpdateID + 1
            }
        }
    }
}

// isAdmin checks if user is authorized
func (b *Bot) isAdmin(userID int64) bool {
    for _, id := range b.admins {
        if id == userID {
            return true
        }
    }
    return false
}

// checkAdminAccess validates admin access
func (b *Bot) checkAdminAccess(ctx context.Context, chatID int64, userID int64) bool {
    if !b.isAdmin(userID) {
        b.sendUnauthorized(ctx, chatID)
        return false
    }
    return true
}

// sendUnauthorized sends access denied message
func (b *Bot) sendUnauthorized(ctx context.Context, chatID int64) {
    b.sendMarkdown(
        ctx,
        chatID,
        "⛔ *Access Denied*\n\n"+
            "You are not authorized to use this bot.\n\n"+
            "🔒 This bot is for administrators only.",
    )
}