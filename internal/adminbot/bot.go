package adminbot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"babibingo/internal/config"
	"babibingo/internal/game"
	"babibingo/internal/models"

	"github.com/mymmrac/telego"
	"gorm.io/gorm"
)

type Bot struct {
	api         *telego.Bot
	me          *telego.User
	db          *gorm.DB
	cfg         *config.Config
	admins      []int64
	botName     string
	engine      *game.Engine
	startTime   time.Time
	botSettings BotSettings
}

func New(token string, db *gorm.DB, cfg *config.Config, engine *game.Engine) (*Bot, error) {
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

	// Load admin IDs from database first
	var settings AdminConfig
	if err := db.First(&settings).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// Create default settings
			settings = AdminConfig{
				AutoApprove:      false,
				NotifyOnApply:    true,
				NotifyOnDeposit:  true,
				NotifyOnWithdraw: true,
				BotEnabled:       true,
				BotsPerTick:      2,
				MaxBotsPerGame:   50,
				ReserveInterval:  3,
				UpdatedAt:        time.Now(),
			}
			db.Create(&settings)
		}
	}

	// Load admin IDs from database if available, otherwise from config
	var adminIDs []int64
	if settings.AdminIDs != "" {
		for _, part := range strings.Split(settings.AdminIDs, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil {
				adminIDs = append(adminIDs, id)
			}
		}
	}

	// If no admins in database, use config as fallback
	if len(adminIDs) == 0 {
		adminIDs = cfg.GetAdminIDs()
		// Save to database if config has admins
		if len(adminIDs) > 0 {
			idStrings := []string{}
			for _, id := range adminIDs {
				idStrings = append(idStrings, strconv.FormatInt(id, 10))
			}
			settings.AdminIDs = strings.Join(idStrings, ",")
			db.Save(&settings)
		}
	}

	if len(adminIDs) == 0 {
		log.Println("⚠️ WARNING: No admin IDs configured! Set ADMIN_IDS in environment")
	} else {
		log.Printf("✅ Admin IDs loaded: %v", adminIDs)
	}

	b := &Bot{
		api:      api,
		me:       me,
		db:       db,
		cfg:      cfg,
		admins:   adminIDs,
		botName:  me.Username,
		engine:   engine,
		startTime: time.Now(),
		botSettings: BotSettings{
			DesiredCount: 20,
			Speed:        2,
			MaxBots:      50,
		},
	}

	// Set commands
	if err := b.setupCommands(ctx); err != nil {
		log.Printf("Failed to set commands: %v", err)
	}

	log.Printf("🤖 Admin Bot started: @%s", me.Username)
	log.Printf("🔐 Admin access: %d admins configured", len(adminIDs))
	log.Printf("📊 Bot settings loaded: Desired=%d, Speed=%d, Max=%d",
		b.botSettings.DesiredCount, b.botSettings.Speed, b.botSettings.MaxBots)

	// ✅ Send startup notification to admins
	go b.notifyAdminsStartup(ctx)

	return b, nil
}

func (b *Bot) setupCommands(ctx context.Context) error {
	commands := []telego.BotCommand{
		{Command: "start", Description: "🏠 Start admin bot"},
		{Command: "help", Description: "❓ Show help menu"},
		{Command: "dashboard", Description: "📊 Show admin dashboard"},
		{Command: "agents", Description: "👥 Manage agents"},
		{Command: "deposits", Description: "💳 Manage deposits"},
		{Command: "withdrawals", Description: "🏧 Manage withdrawals"},
		{Command: "games", Description: "🎱 Monitor games"},
		{Command: "bots", Description: "🤖 Manage game bots"},
		{Command: "users", Description: "👤 Manage users"},
		{Command: "stats", Description: "📈 View statistics"},
		{Command: "settings", Description: "⚙️ Bot settings"},
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

// ✅ Notify admins when bot starts
func (b *Bot) notifyAdminsStartup(ctx context.Context) {
	time.Sleep(2 * time.Second) // Wait for bot to fully start

	// Get some stats for the startup message
	var totalUsers int64
	b.db.Model(&models.User{}).Count(&totalUsers)

	var totalAgents int64
	b.db.Model(&models.User{}).Where("is_agent = ?", true).Count(&totalAgents)

	var totalGames int64
	b.db.Model(&models.Game{}).Count(&totalGames)

	message := fmt.Sprintf(
		"🤖 *Admin Bot Started*\n\n"+
			"🕐 Time: %s\n"+
			"👥 Admins: %d\n"+
			"📊 Total Users: %d\n"+
			"🤝 Agents: %d\n"+
			"🎱 Games: %d\n\n"+
			"📈 Bot Status: ✅ Running\n"+
			"🔧 Settings: Auto-approve=%v\n\n"+
			"Use /dashboard to see the admin panel.",
		time.Now().Format("Jan 2, 2006 15:04:05"),
		len(b.admins),
		totalUsers,
		totalAgents,
		totalGames,
		b.botSettings.AutoApprove,
	)

	for _, adminID := range b.admins {
		b.sendMarkdown(ctx, adminID, message)
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
			"🔒 This bot is for administrators only.\n\n"+
			"👥 If you are an admin, please contact the system administrator.",
	)
}

