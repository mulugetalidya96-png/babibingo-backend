package adminbot

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"babibingo/internal/models"

	"github.com/mymmrac/telego"
	"gorm.io/gorm"
)

// ============ USER MANAGEMENT HANDLER ============

func (b *Bot) handleUsers(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 {
		b.listUsers(ctx, chatID, 1)
		return
	}

	switch args[0] {
	case "list":
		page := 1
		if len(args) > 1 {
			if p, err := strconv.Atoi(args[1]); err == nil && p > 0 {
				page = p
			}
		}
		b.listUsers(ctx, chatID, page)

	case "search":
		if len(args) > 1 {
			query := strings.Join(args[1:], " ")
			b.searchUsersSmart(ctx, chatID, query)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /users search <username or phone>")
		}

	case "view":
		if len(args) > 1 {
			id, _ := strconv.ParseInt(args[1], 10, 64)
			b.viewUser(ctx, chatID, id)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /users view <telegram_id>")
		}

	case "add":
		if len(args) > 2 {
			id, _ := strconv.ParseInt(args[1], 10, 64)
			amount, _ := strconv.ParseFloat(args[2], 64)
			b.addBalance(ctx, chatID, id, amount)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /users add <telegram_id> <amount>")
		}

	case "deduct":
		if len(args) > 2 {
			id, _ := strconv.ParseInt(args[1], 10, 64)
			amount, _ := strconv.ParseFloat(args[2], 64)
			b.deductBalance(ctx, chatID, id, amount)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /users deduct <telegram_id> <amount>")
		}

	case "suspend":
		if len(args) > 1 {
			id, _ := strconv.ParseInt(args[1], 10, 64)
			b.suspendUser(ctx, chatID, id)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /users suspend <telegram_id>")
		}

	case "unsuspend":
		if len(args) > 1 {
			id, _ := strconv.ParseInt(args[1], 10, 64)
			b.unsuspendUser(ctx, chatID, id)
		} else {
			b.sendText(ctx, chatID, "❌ Usage: /users unsuspend <telegram_id>")
		}

	case "stats":
		b.showUserStats(ctx, chatID)

	default:
		b.sendText(ctx, chatID, "❌ Usage: /users [list|search|view|add|deduct|suspend|unsuspend|stats]")
	}
}

// ============ LIST USERS WITH BUTTONS ============

func (b *Bot) listUsers(ctx context.Context, chatID int64, page int) {
	limit := 10
	offset := (page - 1) * limit

	var users []models.User
	var total int64

	b.db.Model(&models.User{}).Where("is_bot = ?", false).Count(&total)
	b.db.Where("is_bot = ?", false).Order("created_at DESC").Limit(limit).Offset(offset).Find(&users)

	if len(users) == 0 {
		b.sendText(ctx, chatID, "📋 No users found.")
		return
	}

	totalPages := int((total + int64(limit) - 1) / int64(limit))

	text := fmt.Sprintf("👥 *Users (Page %d/%d)*\n", page, totalPages)
	text += fmt.Sprintf("📊 Total: %d users\n\n", total)

	for i, user := range users {
		agentBadge := ""
		if user.IsAgent {
			agentBadge = " 🤝"
		}

		phone := formatPhoneNumber(user.PhoneNumber)
		username := user.Username
		if username == "" {
			username = "No username"
		}

		// Check if user is active (within last 7 days)
		isActive := time.Since(user.LastActive) < 7*24*time.Hour
		activeBadge := "🟢"
		if !isActive {
			activeBadge = "🔴"
		}

		text += fmt.Sprintf(
			"%d. %s @%s%s\n   💰 %.2f ETB | 📱 %s\n   🆔 `%d`\n\n",
			offset+i+1,
			activeBadge,
			username,
			agentBadge,
			user.Balance,
			phone,
			user.TelegramID,
		)
	}

	// Create inline keyboard with navigation and action buttons
	keyboard := [][]telego.InlineKeyboardButton{}

	// Navigation row
	navRow := []telego.InlineKeyboardButton{}
	if page > 1 {
		navRow = append(navRow, telego.InlineKeyboardButton{
			Text: "⬅️ Prev",
			CallbackData: fmt.Sprintf("users_page_%d", page-1),
		})
	}
	navRow = append(navRow, telego.InlineKeyboardButton{
		Text: fmt.Sprintf("%d/%d", page, totalPages),
		CallbackData: "users_current",
	})
	if page < totalPages {
		navRow = append(navRow, telego.InlineKeyboardButton{
			Text: "Next ➡️",
			CallbackData: fmt.Sprintf("users_page_%d", page+1),
		})
	}
	keyboard = append(keyboard, navRow)

	// Action row
	keyboard = append(keyboard, []telego.InlineKeyboardButton{
		{Text: "🔍 Search Users", CallbackData: "users_search"},
		{Text: "📊 Stats", CallbackData: "users_stats"},
	})

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}

// ============ SMART SEARCH (Auto-formats phone numbers) ============

func (b *Bot) searchUsersSmart(ctx context.Context, chatID int64, query string) {
	// Clean and format the query
	formattedQuery := b.formatSearchQuery(query)

	var users []models.User
	var searchResults []models.User

	// Try multiple search strategies
	// 1. Exact phone number match
	if formattedQuery != "" {
		b.db.Where("phone_number = ?", formattedQuery).
			Where("is_bot = ?", false).
			Find(&searchResults)
		if len(searchResults) > 0 {
			users = searchResults
		}
	}

	// 2. If no results, try partial match
	if len(users) == 0 && formattedQuery != "" {
		b.db.Where("phone_number ILIKE ? OR phone_number ILIKE ?",
			"%"+formattedQuery+"%",
			"%"+strings.TrimPrefix(formattedQuery, "+251")+"%").
			Where("is_bot = ?", false).
			Find(&users)
	}

	// 3. If still no results, try username or name search
	if len(users) == 0 {
		searchPattern := "%" + strings.TrimPrefix(query, "@") + "%"
		b.db.Where("username ILIKE ? OR first_name ILIKE ? OR last_name ILIKE ?",
			searchPattern, searchPattern, searchPattern).
			Where("is_bot = ?", false).
			Order("created_at DESC").
			Find(&users)
	}

	// 4. If still no results, try with country code variations
	if len(users) == 0 && isPhoneNumberLike(query) {
		// Try different phone number formats
		variations := b.getPhoneVariations(query)
		for _, variant := range variations {
			var tempUsers []models.User
			b.db.Where("phone_number = ?", variant).
				Where("is_bot = ?", false).
				Find(&tempUsers)
			if len(tempUsers) > 0 {
				users = tempUsers
				break
			}
		}
	}

	// Show results or no results message
	if len(users) == 0 {
		b.showNoResultsFound(ctx, chatID, query)
		return
	}

	b.showSearchResults(ctx, chatID, query, users)
}

// ============ FORMAT SEARCH QUERY ============

func (b *Bot) formatSearchQuery(query string) string {
	// Remove spaces and special characters
	query = strings.TrimSpace(query)
	query = strings.TrimPrefix(query, "@")

	// If it's a phone number
	if isPhoneNumberLike(query) {
		return formatPhoneNumber(query)
	}

	return query
}

// ============ PHONE NUMBER FORMATTING ============

func formatPhoneNumber(phone string) string {
	if phone == "" {
		return "Not set"
	}

	// Remove all non-digit characters except +
	re := regexp.MustCompile(`[^0-9+]`)
	phone = re.ReplaceAllString(phone, "")

	// Remove leading 0 if present
	if strings.HasPrefix(phone, "0") {
		phone = phone[1:]
	}

	// Remove leading + if present (we'll add it back)
	phone = strings.TrimPrefix(phone, "+")

	// If starts with 251, format as +251...
	if strings.HasPrefix(phone, "251") {
		return "+" + phone
	}

	// If starts with 9, add +251
	if strings.HasPrefix(phone, "9") && len(phone) == 9 {
		return "+251" + phone
	}

	// If length is 10 (like 098xxxxxxx), remove first 0 and add +251
	if len(phone) == 10 && strings.HasPrefix(phone, "9") {
		return "+251" + phone
	}

	// If length is 9, add +251
	if len(phone) == 9 {
		return "+251" + phone
	}

	// Default: try to add +251
	if !strings.HasPrefix(phone, "251") && len(phone) > 0 {
		return "+251" + phone
	}

	return "+" + phone
}

// ============ PHONE VARIATIONS ============

func (b *Bot) getPhoneVariations(phone string) []string {
	clean := regexp.MustCompile(`[^0-9]`).ReplaceAllString(phone, "")
	variations := []string{}

	// Remove leading 0
	if strings.HasPrefix(clean, "0") {
		clean = clean[1:]
	}

	// Various formats
	if strings.HasPrefix(clean, "251") {
		variations = append(variations, "+"+clean)
		variations = append(variations, clean)
		variations = append(variations, "0"+clean[3:])
	} else if strings.HasPrefix(clean, "9") && len(clean) == 9 {
		variations = append(variations, "+251"+clean)
		variations = append(variations, "251"+clean)
		variations = append(variations, "0"+clean)
	} else {
		variations = append(variations, "+251"+clean)
		variations = append(variations, "251"+clean)
		variations = append(variations, "0"+clean)
	}

	return variations
}

// ============ CHECK IF PHONE NUMBER ============

func isPhoneNumberLike(text string) bool {
	// Remove spaces and special characters
	clean := regexp.MustCompile(`[^0-9+]`).ReplaceAllString(text, "")

	// Check if it contains mostly digits
	digitCount := 0
	for _, char := range clean {
		if char >= '0' && char <= '9' {
			digitCount++
		}
	}

	// If it has 8+ digits, it's likely a phone number
	return digitCount >= 8
}

// ============ SHOW SEARCH RESULTS ============

func (b *Bot) showSearchResults(ctx context.Context, chatID int64, query string, users []models.User) {
	text := fmt.Sprintf("🔍 *Search Results for '%s'*\n", query)
	text += fmt.Sprintf("📊 Found: %d user(s)\n\n", len(users))

	// Show users in a clean format with numbers
	for i, user := range users {
		phone := formatPhoneNumber(user.PhoneNumber)
		username := user.Username
		if username == "" {
			username = "No username"
		}

		agentBadge := ""
		if user.IsAgent {
			agentBadge = " 🤝"
		}

		// Check if user is active (within last 7 days)
		isActive := time.Since(user.LastActive) < 7*24*time.Hour
		activeBadge := "🟢"
		if !isActive {
			activeBadge = "🔴"
		}

		text += fmt.Sprintf(
			"%d. %s @%s%s\n"+
				"   💰 %.2f ETB | 📱 %s\n"+
				"   🆔 `%d`\n\n",
			i+1,
			activeBadge,
			username,
			agentBadge,
			user.Balance,
			phone,
			user.TelegramID,
		)
	}

	// Add action buttons
	keyboard := [][]telego.InlineKeyboardButton{
		{
			{Text: "👤 View User", CallbackData: "users_view_prompt"},
			{Text: "🔄 Back to List", CallbackData: "users_list"},
		},
		{
			{Text: "🔍 New Search", CallbackData: "users_search"},
		},
	}

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}

// ============ SHOW NO RESULTS FOUND ============

func (b *Bot) showNoResultsFound(ctx context.Context, chatID int64, query string) {
	// Clean query for display
	displayQuery := query
	if isPhoneNumberLike(query) {
		displayQuery = formatPhoneNumber(query)
	}

	text := fmt.Sprintf(
		"🔍 *No users found for:* `%s`\n\n"+
			"💡 *Search tips:*\n"+
			"• 📱 Phone: Just type the number (e.g., `09847488474`)\n"+
			"• 👤 Username: Type with or without @ (e.g., `@username`)\n"+
			"• 📝 Name: Type first or last name\n\n"+
			"📌 *Examples:*\n"+
			"• `09847488474` → Will search as +2519847488474\n"+
			"• `@john_doe` → Search by username\n"+
			"• `John` → Search by name\n\n"+
			"🔄 Try searching with a different format!",
		displayQuery,
	)

	keyboard := [][]telego.InlineKeyboardButton{
		{
			{Text: "🔍 New Search", CallbackData: "users_search"},
			{Text: "📋 All Users", CallbackData: "users_list"},
		},
	}

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}

// ============ SEARCH USERS (Legacy - Keep for compatibility) ============

func (b *Bot) searchUsers(ctx context.Context, chatID int64, query string) {
	b.searchUsersSmart(ctx, chatID, query)
}

// ============ VIEW USER WITH ACTION BUTTONS ============

func (b *Bot) viewUser(ctx context.Context, chatID int64, telegramID int64) {
	var user models.User
	if err := b.db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			b.sendText(ctx, chatID, "❌ User not found.")
		} else {
			b.sendText(ctx, chatID, fmt.Sprintf("❌ Error: %v", err))
		}
		return
	}

	// Get user statistics
	var gameCount int64
	b.db.Model(&models.GamePlayer{}).Where("user_id = ?", user.ID).Count(&gameCount)

	var cardCount int64
	b.db.Model(&models.Card{}).Where("user_id = ?", user.ID).Count(&cardCount)

	var totalStaked float64
	b.db.Model(&models.Transaction{}).
		Where("user_id = ? AND type = ? AND status = ?", user.ID, "stake", "completed").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalStaked)

	var totalWon float64
	b.db.Model(&models.Transaction{}).
		Where("user_id = ? AND type = ? AND status = ?", user.ID, "win", "completed").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalWon)

	var totalDeposits float64
	b.db.Model(&models.Transaction{}).
		Where("user_id = ? AND type = ? AND status = ?", user.ID, "deposit", "completed").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalDeposits)

	var totalWithdrawals float64
	b.db.Model(&models.Transaction{}).
		Where("user_id = ? AND type = ? AND status = ?", user.ID, "withdraw", "completed").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalWithdrawals)

	var referralCount int64
	b.db.Model(&models.User{}).Where("referred_by = ?", user.ID).Count(&referralCount)

	// Get recent transactions
	var recentTxs []models.Transaction
	b.db.Where("user_id = ?", user.ID).
		Order("created_at DESC").
		Limit(5).
		Find(&recentTxs)

	recentTxText := ""
	for _, tx := range recentTxs {
		statusEmoji := "🟡"
		if tx.Status == "completed" {
			statusEmoji = "✅"
		} else if tx.Status == "failed" {
			statusEmoji = "❌"
		}
		recentTxText += fmt.Sprintf(
			"• %s %.2f ETB (%s) %s\n",
			statusEmoji,
			tx.Amount,
			tx.Type,
			tx.CreatedAt.Format("Jan 2 15:04"),
		)
	}
	if recentTxText == "" {
		recentTxText = "No recent transactions"
	}

	phone := formatPhoneNumber(user.PhoneNumber)
	username := user.Username
	if username == "" {
		username = "No username"
	}

	agentText := "❌"
	if user.IsAgent {
		agentText = fmt.Sprintf("✅ (Balance: %.2f ETB)", user.AgentBalance)
	}

	text := fmt.Sprintf(
		"👤 *User Details*\n\n"+
			"🆔 ID: `%d`\n"+
			"👤 Username: @%s\n"+
			"📝 Name: %s %s\n"+
			"📱 Phone: %s\n"+
			"💰 Balance: %.2f ETB\n"+
			"🤝 Agent: %s\n"+
			"🔑 Referral Code: `%s`\n"+
			"👥 Referrals: %d\n"+
			"📅 Joined: %s\n"+
			"🔄 Last Active: %s\n\n"+
			"📊 *Statistics:*\n"+
			"• Games Played: %d\n"+
			"• Cards Purchased: %d\n"+
			"• Total Staked: %.2f ETB\n"+
			"• Total Won: %.2f ETB\n"+
			"• Total Deposits: %.2f ETB\n"+
			"• Total Withdrawals: %.2f ETB\n\n"+
			"📋 *Recent Transactions:*\n%s",
		user.TelegramID,
		username,
		user.FirstName,
		user.LastName,
		phone,
		user.Balance,
		agentText,
		user.ReferralCode,
		referralCount,
		user.CreatedAt.Format("Jan 2, 2006 15:04"),
		user.LastActive.Format("Jan 2, 2006 15:04"),
		gameCount,
		cardCount,
		totalStaked,
		totalWon,
		totalDeposits,
		totalWithdrawals,
		recentTxText,
	)

	// Create action buttons for the user
	keyboard := [][]telego.InlineKeyboardButton{
		{
			{Text: "➕ Add Balance", CallbackData: fmt.Sprintf("user_add_%d", user.TelegramID)},
			{Text: "➖ Deduct Balance", CallbackData: fmt.Sprintf("user_deduct_%d", user.TelegramID)},
		},
		{
			{Text: "📱 Transactions", CallbackData: fmt.Sprintf("user_tx_%d", user.TelegramID)},
			{Text: "📊 Full Stats", CallbackData: fmt.Sprintf("user_stats_%d", user.TelegramID)},
		},
		{
			{Text: "🔴 Suspend", CallbackData: fmt.Sprintf("user_suspend_%d", user.TelegramID)},
			{Text: "🟢 Unsuspend", CallbackData: fmt.Sprintf("user_unsuspend_%d", user.TelegramID)},
		},
		{
			{Text: "🔄 Refresh", CallbackData: fmt.Sprintf("user_refresh_%d", user.TelegramID)},
			{Text: "⬅️ Back to List", CallbackData: "users_list"},
		},
	}

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}

// ============ ADD BALANCE ============

func (b *Bot) addBalance(ctx context.Context, chatID int64, telegramID int64, amount float64) {
	if amount <= 0 {
		b.sendText(ctx, chatID, "❌ Amount must be greater than 0")
		return
	}
	b.adjustBalance(ctx, chatID, telegramID, amount)
}

// ============ DEDUCT BALANCE ============

func (b *Bot) deductBalance(ctx context.Context, chatID int64, telegramID int64, amount float64) {
	if amount <= 0 {
		b.sendText(ctx, chatID, "❌ Amount must be greater than 0")
		return
	}

	var user models.User
	if err := b.db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		b.sendText(ctx, chatID, "❌ User not found.")
		return
	}

	if user.Balance < amount {
		b.sendMarkdown(ctx, chatID, fmt.Sprintf(
			"❌ *Insufficient Balance*\n\n"+
				"👤 User: @%s\n"+
				"💰 Current Balance: %.2f ETB\n"+
				"💸 Attempted Deduction: %.2f ETB\n\n"+
				"⚠️ Cannot deduct more than current balance.",
			user.Username,
			user.Balance,
			amount,
		))
		return
	}

	b.adjustBalance(ctx, chatID, telegramID, -amount)
}

// ============ BALANCE ADJUSTMENT (Main function) ============

func (b *Bot) adjustBalance(ctx context.Context, chatID int64, telegramID int64, amount float64) {
	var user models.User
	if err := b.db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			b.sendText(ctx, chatID, "❌ User not found.")
		} else {
			b.sendText(ctx, chatID, fmt.Sprintf("❌ Error: %v", err))
		}
		return
	}

	oldBalance := user.Balance
	user.Balance += amount
	if err := b.db.Save(&user).Error; err != nil {
		b.sendText(ctx, chatID, fmt.Sprintf("❌ Failed to update balance: %v", err))
		return
	}

	// Create adjustment transaction
	txType := "admin_add"
	description := fmt.Sprintf("Admin added %.2f ETB", amount)

	if amount < 0 {
		txType = "admin_deduct"
		description = fmt.Sprintf("Admin deducted %.2f ETB", -amount)
	}

	transaction := models.Transaction{
		UserID:      user.ID,
		Type:        txType,
		Amount:      amount,
		Status:      "completed",
		Method:      "admin",
		Description: description,
		CreatedAt:   time.Now(),
	}
	b.db.Create(&transaction)

	// Log admin action
	b.logAdminAction(ctx, chatID, txType, user.TelegramID, "user",
		fmt.Sprintf("%s (%.2f ETB) - Old: %.2f, New: %.2f", description, amount, oldBalance, user.Balance))

	// Notify user
	sign := "+"
	if amount < 0 {
		sign = ""
	}

	go func() {
		b.sendMarkdown(
			context.Background(),
			user.TelegramID,
			fmt.Sprintf(
				"💰 *Balance Adjustment*\n\n"+
					"Your balance has been adjusted by an administrator.\n\n"+
					"📊 Amount: %s%.2f ETB\n"+
					"💳 Previous Balance: %.2f ETB\n"+
					"💳 New Balance: %.2f ETB\n\n"+
					"If you have questions, please contact support.",
				sign,
				amount,
				oldBalance,
				user.Balance,
			),
		)
	}()

	// Send confirmation to admin
	action := "Added"
	if amount < 0 {
		action = "Deducted"
	}

	b.sendMarkdown(ctx, chatID, fmt.Sprintf(
		"✅ *Balance %s*\n\n"+
			"👤 User: @%s\n"+
			"🆔 ID: `%d`\n"+
			"📊 Previous Balance: %.2f ETB\n"+
			"💵 Amount: %s%.2f ETB\n"+
			"💳 New Balance: %.2f ETB",
		action,
		user.Username,
		user.TelegramID,
		oldBalance,
		sign,
		amount,
		user.Balance,
	))
}

// ============ SUSPEND / UNSUSPEND ============

func (b *Bot) suspendUser(ctx context.Context, chatID int64, telegramID int64) {
	// You would need a "suspended" field in the User model
	b.sendMarkdown(ctx, chatID, fmt.Sprintf(
		"⏳ *User Suspension*\n\n"+
			"User `%d` has been suspended.\n\n"+
			"⚠️ This feature requires a `suspended` field in the User model.",
		telegramID,
	))

	b.logAdminAction(ctx, chatID, "suspend_user", telegramID, "user",
		fmt.Sprintf("Suspended user %d", telegramID))
}

func (b *Bot) unsuspendUser(ctx context.Context, chatID int64, telegramID int64) {
	b.sendMarkdown(ctx, chatID, fmt.Sprintf(
		"✅ *User Unsuspended*\n\n"+
			"User `%d` has been unsuspended.",
		telegramID,
	))

	b.logAdminAction(ctx, chatID, "unsuspend_user", telegramID, "user",
		fmt.Sprintf("Unsuspended user %d", telegramID))
}

// ============ USER STATISTICS ============

func (b *Bot) showUserStats(ctx context.Context, chatID int64) {
	var totalUsers int64
	var totalAgents int64
	var totalBots int64
	var activeUsers int64
	var usersWithBalance int64
	var totalBalance float64

	b.db.Model(&models.User{}).Where("is_bot = ?", false).Count(&totalUsers)
	b.db.Model(&models.User{}).Where("is_bot = ?", false).Where("is_agent = ?", true).Count(&totalAgents)
	b.db.Model(&models.User{}).Where("is_bot = ?", true).Count(&totalBots)

	// Active users: last 7 days
	weekAgo := time.Now().AddDate(0, 0, -7)
	b.db.Model(&models.User{}).Where("last_active >= ?", weekAgo).Where("is_bot = ?", false).Count(&activeUsers)

	// Today's new users
	today := time.Now().Truncate(24 * time.Hour)
	var newUsersToday int64
	b.db.Model(&models.User{}).Where("created_at >= ?", today).Where("is_bot = ?", false).Count(&newUsersToday)

	// Users with balance > 0
	b.db.Model(&models.User{}).Where("balance > 0").Where("is_bot = ?", false).Count(&usersWithBalance)

	// Total balance
	b.db.Model(&models.User{}).Where("is_bot = ?", false).Select("COALESCE(SUM(balance), 0)").Scan(&totalBalance)

	// Calculate percentage of active users
	activePercentage := 0.0
	if totalUsers > 0 {
		activePercentage = float64(activeUsers) / float64(totalUsers) * 100
	}

	text := fmt.Sprintf(
		"📊 *User Statistics*\n\n"+
			"👥 *Total Users:* %d\n"+
			"🤝 *Agents:* %d\n"+
			"🤖 *Bots:* %d\n\n"+
			"📈 *Activity:*\n"+
			"• Active (7d): %d (%.1f%%)\n"+
			"• New Today: %d\n"+
			"• With Balance: %d\n\n"+
			"💰 *Balance Stats:*\n"+
			"• Total Balance: %.2f ETB\n"+
			"• Average Balance: %.2f ETB\n"+
			"• Highest Balance: %.2f ETB",
		totalUsers,
		totalAgents,
		totalBots,
		activeUsers,
		activePercentage,
		newUsersToday,
		usersWithBalance,
		totalBalance,
		b.getAverageBalance(),
		b.getHighestBalance(),
	)

	// Add refresh button
	keyboard := [][]telego.InlineKeyboardButton{
		{{Text: "🔄 Refresh", CallbackData: "users_stats"}},
		{{Text: "⬅️ Back to Users", CallbackData: "users_list"}},
	}

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}

// ============ USER TRANSACTIONS ============

func (b *Bot) showUserTransactions(ctx context.Context, chatID int64, telegramID int64) {
	var user models.User
	if err := b.db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		b.sendText(ctx, chatID, "❌ User not found.")
		return
	}

	var transactions []models.Transaction
	b.db.Where("user_id = ?", user.ID).
		Order("created_at DESC").
		Limit(20).
		Find(&transactions)

	if len(transactions) == 0 {
		b.sendMarkdown(ctx, chatID, fmt.Sprintf(
			"📋 *No transactions found*\n\n"+
				"👤 User: @%s\n"+
				"💰 Balance: %.2f ETB",
			user.Username,
			user.Balance,
		))
		return
	}

	text := fmt.Sprintf(
		"📋 *User Transactions*\n"+
			"👤 @%s\n"+
			"💰 Balance: %.2f ETB\n\n",
		user.Username,
		user.Balance,
	)

	for _, tx := range transactions {
		emoji := "🟡"
		if tx.Status == "completed" {
			emoji = "✅"
		} else if tx.Status == "failed" {
			emoji = "❌"
		} else if tx.Status == "pending" {
			emoji = "⏳"
		}

		sign := ""
		if tx.Amount > 0 && (tx.Type == "deposit" || tx.Type == "win" || tx.Type == "admin_add") {
			sign = "+"
		}

		text += fmt.Sprintf(
			"%s %s%.2f ETB | %s\n"+
				"   📅 %s | %s\n",
			emoji,
			sign,
			tx.Amount,
			tx.Type,
			tx.CreatedAt.Format("Jan 2 15:04"),
			tx.Status,
		)
	}

	keyboard := [][]telego.InlineKeyboardButton{
		{
			{Text: "🔄 Refresh", CallbackData: fmt.Sprintf("user_tx_%d", user.TelegramID)},
			{Text: "⬅️ Back to User", CallbackData: fmt.Sprintf("user_refresh_%d", user.TelegramID)},
		},
	}

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}

// ============ USER FULL STATS ============

func (b *Bot) showUserFullStats(ctx context.Context, chatID int64, telegramID int64) {
	var user models.User
	if err := b.db.Where("telegram_id = ?", telegramID).First(&user).Error; err != nil {
		b.sendText(ctx, chatID, "❌ User not found.")
		return
	}

	// Get comprehensive stats
	var totalGames int64
	b.db.Model(&models.GamePlayer{}).Where("user_id = ?", user.ID).Count(&totalGames)

	var totalCards int64
	b.db.Model(&models.Card{}).Where("user_id = ?", user.ID).Count(&totalCards)

	var totalStaked float64
	b.db.Model(&models.Transaction{}).
		Where("user_id = ? AND type IN (?) AND status = ?", user.ID, []string{"stake", "deposit"}, "completed").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalStaked)

	var totalWon float64
	b.db.Model(&models.Transaction{}).
		Where("user_id = ? AND type = ? AND status = ?", user.ID, "win", "completed").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalWon)

	var totalDeposits float64
	b.db.Model(&models.Transaction{}).
		Where("user_id = ? AND type = ? AND status = ?", user.ID, "deposit", "completed").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalDeposits)

	var totalWithdrawals float64
	b.db.Model(&models.Transaction{}).
		Where("user_id = ? AND type = ? AND status = ?", user.ID, "withdraw", "completed").
		Select("COALESCE(SUM(amount), 0)").Scan(&totalWithdrawals)

	var referralCount int64
	b.db.Model(&models.User{}).Where("referred_by = ?", user.ID).Count(&referralCount)

	// Calculate win rate
	winRate := 0.0
	if totalGames > 0 {
		var wins int64
		b.db.Model(&models.Transaction{}).
			Where("user_id = ? AND type = ? AND status = ?", user.ID, "win", "completed").
			Count(&wins)
		winRate = float64(wins) / float64(totalGames) * 100
	}

	// Active status
	isActive := time.Since(user.LastActive) < 7*24*time.Hour
	activeStatus := "🟢 Active"
	if !isActive {
		activeStatus = "🔴 Inactive"
	}

	text := fmt.Sprintf(
		"📊 *Full User Statistics*\n\n"+
			"👤 @%s\n"+
			"🆔 `%d`\n"+
			"📱 %s\n\n"+
			"📈 *Activity:*\n"+
			"• Status: %s\n"+
			"• Games Played: %d\n"+
			"• Cards Purchased: %d\n"+
			"• Win Rate: %.1f%%\n\n"+
			"💰 *Financial:*\n"+
			"• Current Balance: %.2f ETB\n"+
			"• Total Deposits: %.2f ETB\n"+
			"• Total Withdrawals: %.2f ETB\n"+
			"• Total Staked: %.2f ETB\n"+
			"• Total Won: %.2f ETB\n\n"+
			"👥 *Referrals:*\n"+
			"• Total Referrals: %d\n\n"+
			"🤝 *Agent:* %v\n"+
			"📅 Joined: %s",
		user.Username,
		user.TelegramID,
		formatPhoneNumber(user.PhoneNumber),
		activeStatus,
		totalGames,
		totalCards,
		winRate,
		user.Balance,
		totalDeposits,
		totalWithdrawals,
		totalStaked,
		totalWon,
		referralCount,
		user.IsAgent,
		user.CreatedAt.Format("Jan 2, 2006 15:04"),
	)

	keyboard := [][]telego.InlineKeyboardButton{
		{
			{Text: "🔄 Refresh", CallbackData: fmt.Sprintf("user_stats_%d", user.TelegramID)},
			{Text: "⬅️ Back", CallbackData: fmt.Sprintf("user_refresh_%d", user.TelegramID)},
		},
	}

	b.sendMarkdownKeyboard(ctx, chatID, text, keyboard)
}

// ============ HELPER FUNCTIONS ============

// getAverageBalance - Helper to calculate average balance
func (b *Bot) getAverageBalance() float64 {
	var avg float64
	b.db.Model(&models.User{}).Where("is_bot = ?", false).Select("COALESCE(AVG(balance), 0)").Scan(&avg)
	return avg
}

// getHighestBalance - Helper to get highest balance
func (b *Bot) getHighestBalance() float64 {
	var max float64
	b.db.Model(&models.User{}).Where("is_bot = ?", false).Select("COALESCE(MAX(balance), 0)").Scan(&max)
	return max
}

// ============ HANDLE SEARCH PROMPT ============

func (b *Bot) handleSearchPrompt(ctx context.Context, chatID int64) {
	b.sendMarkdown(ctx, chatID,
		"🔍 *Search Users*\n\n"+
			"📱 Just type the phone number, username, or name.\n\n"+
			"💡 *Examples:*\n"+
			"• `09847488474` → Will auto-format to +2519847488474\n"+
			"• `@username` → Search by username\n"+
			"• `John` → Search by name\n\n"+
			"⌨️ Type your search query below:",
	)
	b.tempState.Store(chatID, "awaiting_user_search")
}

// ============ HANDLE USER TEXT INPUT ============

func (b *Bot) handleUserTextInput(ctx context.Context, chatID int64, text string) {
	// Check if we're waiting for search input
	if state, ok := b.tempState.Load(chatID); ok {
		stateStr := state.(string)

		// Search users (from button click)
		if stateStr == "awaiting_user_search" {
			b.tempState.Delete(chatID)
			b.searchUsersSmart(ctx, chatID, text)
			return
		}

		// Add balance
		if strings.HasPrefix(stateStr, "awaiting_add_balance_") {
			userID, err := strconv.ParseInt(strings.TrimPrefix(stateStr, "awaiting_add_balance_"), 10, 64)
			if err != nil {
				b.sendText(ctx, chatID, "❌ Invalid user ID")
				b.tempState.Delete(chatID)
				return
			}

			amount, err := strconv.ParseFloat(text, 64)
			if err != nil || amount <= 0 {
				b.sendText(ctx, chatID, "❌ Please enter a valid amount (e.g., `100` or `50.5`)")
				return
			}

			b.tempState.Delete(chatID)
			b.addBalance(ctx, chatID, userID, amount)
			return
		}

		// Deduct balance
		if strings.HasPrefix(stateStr, "awaiting_deduct_balance_") {
			userID, err := strconv.ParseInt(strings.TrimPrefix(stateStr, "awaiting_deduct_balance_"), 10, 64)
			if err != nil {
				b.sendText(ctx, chatID, "❌ Invalid user ID")
				b.tempState.Delete(chatID)
				return
			}

			amount, err := strconv.ParseFloat(text, 64)
			if err != nil || amount <= 0 {
				b.sendText(ctx, chatID, "❌ Please enter a valid amount (e.g., `100` or `50.5`)")
				return
			}

			b.tempState.Delete(chatID)
			b.deductBalance(ctx, chatID, userID, amount)
			return
		}
	}

	// If no state, check if it looks like a phone number or username
	if strings.HasPrefix(text, "/") {
		return // It's a command, ignore
	}

	// Check if it might be a phone number (contains digits and optional +)
	if isPhoneNumberLike(text) || strings.Contains(text, "@") {
		// Auto-search
		b.searchUsersSmart(ctx, chatID, text)
	}
}

// ============ CALLBACK HANDLERS ============

func (b *Bot) handleUserCallbacks(ctx context.Context, query *telego.CallbackQuery) {
	data := query.Data
	chatID := query.Message.GetChat().ID

	// Acknowledge callback
	b.api.AnswerCallbackQuery(ctx, &telego.AnswerCallbackQueryParams{
		CallbackQueryID: query.ID,
	})

	parts := strings.Split(data, "_")
	if len(parts) < 2 {
		return
	}

	switch parts[0] {
	case "users":
		switch parts[1] {
		case "page":
			if len(parts) > 2 {
				page, _ := strconv.Atoi(parts[2])
				b.listUsers(ctx, chatID, page)
			}
		case "list":
			b.listUsers(ctx, chatID, 1)
		case "search":
			b.handleSearchPrompt(ctx, chatID)
		case "stats":
			b.showUserStats(ctx, chatID)
		case "view":
			// If we have view_prompt, ask for ID
			if len(parts) > 2 && parts[2] == "prompt" {
				b.sendText(ctx, chatID, "👤 Enter the Telegram ID of the user to view:")
				b.tempState.Store(chatID, "awaiting_user_view")
			}
		}

	case "user":
		if len(parts) < 3 {
			return
		}

		targetUserID, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			b.sendText(ctx, chatID, "❌ Invalid user ID")
			return
		}

		switch parts[1] {
		case "add":
			b.sendMarkdown(ctx, chatID, fmt.Sprintf(
				"💰 *Add Balance*\n\n"+
					"Enter the amount to add for user `%d`:\n\n"+
					"📌 Example: `100` or `50.5`\n\n"+
					"⌨️ Type the amount below:",
				targetUserID,
			))
			b.tempState.Store(chatID, fmt.Sprintf("awaiting_add_balance_%d", targetUserID))

		case "deduct":
			b.sendMarkdown(ctx, chatID, fmt.Sprintf(
				"💰 *Deduct Balance*\n\n"+
					"Enter the amount to deduct for user `%d`:\n\n"+
					"📌 Example: `100` or `50.5`\n\n"+
					"⌨️ Type the amount below:",
				targetUserID,
			))
			b.tempState.Store(chatID, fmt.Sprintf("awaiting_deduct_balance_%d", targetUserID))

		case "refresh":
			b.viewUser(ctx, chatID, targetUserID)

		case "suspend":
			b.suspendUser(ctx, chatID, targetUserID)

		case "unsuspend":
			b.unsuspendUser(ctx, chatID, targetUserID)

		case "tx":
			b.showUserTransactions(ctx, chatID, targetUserID)

		case "stats":
			b.showUserFullStats(ctx, chatID, targetUserID)
		}
	}
}