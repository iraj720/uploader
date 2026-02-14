package bot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aliebadimehr/telegram-uploader-bot/internal/link"
	repository "github.com/aliebadimehr/telegram-uploader-bot/internal/repository"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/lib/pq"
	"gopkg.in/yaml.v3"
)

var (
	mentionRe = regexp.MustCompile(`@\w+`)

	guideShort   = "Use the buttons below to see how to upload files or how to get the download link."
	guideUpload  = "📤 *How to upload & get link*\n\n1. Send me a *video*, *document*, or *photo* (as a file).\n2. Optionally add a caption (e.g. @username).\n3. I will reply with a *link* (e.g. https://t.me/YourBot?start=xxx).\n4. Share that link with anyone; when they open it, they get the file (after joining your channels if required).\n\nAuthenticate first with `/login <password>` (the password is stored in the bot config)."
	guideGetLink = "🔗 *How to get the file from a link*\n\n1. Open the link you received (e.g. https://t.me/YourBot?start=xxx).\n2. If asked, join the required channels using the buttons, then press Start again or open the link again.\n3. The bot will send you the file. Videos are deleted after a short time; save them if needed."
)

type Config struct {
	APIToken          string   `yaml:"api_token"`
	BotUsername       string   `yaml:"bot_username"`
	DefaultTag        string   `yaml:"default_tag"`
	AdminPassword     string   `yaml:"admin_password"`
	DeleteDelay       int      `yaml:"delete_delay"`
	DBHost            string   `yaml:"db_host"`
	DBPort            int      `yaml:"db_port"`
	DBUser            string   `yaml:"db_user"`
	DBPassword        string   `yaml:"db_password"`
	DBName            string   `yaml:"db_name"`
	DBSSLMode         string   `yaml:"db_sslmode"`
	SponsoredChannels []string `yaml:"sponsored_channels"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}

	if cfg.DeleteDelay == 0 {
		cfg.DeleteDelay = 30
	}
	if cfg.DBHost == "" {
		cfg.DBHost = "postgres"
	}
	if cfg.DBPort == 0 {
		cfg.DBPort = 5432
	}
	if cfg.DBUser == "" {
		cfg.DBUser = "postgres"
	}
	if cfg.DBPassword == "" {
		cfg.DBPassword = "postgres"
	}
	if cfg.DBName == "" {
		cfg.DBName = "uploader"
	}
	if cfg.DBSSLMode == "" {
		cfg.DBSSLMode = "disable"
	}

	cleanedSponsors := make([]string, 0, len(cfg.SponsoredChannels))
	for _, sponsor := range cfg.SponsoredChannels {
		if sponsor = strings.TrimSpace(sponsor); sponsor != "" {
			cleanedSponsors = append(cleanedSponsors, sponsor)
		}
	}
	cfg.SponsoredChannels = cleanedSponsors
	if len(cfg.SponsoredChannels) == 0 || cfg.BotUsername == "" || cfg.APIToken == "" || cfg.AdminPassword == "" {
		return nil, errors.New("config missing required fields (api_token, bot_username, sponsored_channels, admin_password)")
	}
	return &cfg, nil
}

func (cfg *Config) databaseDSN() string {
	if env := os.Getenv("POSTGRES_DSN"); env != "" {
		return env
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		cfg.DBUser,
		cfg.DBPassword,
		cfg.DBHost,
		cfg.DBPort,
		cfg.DBName,
		cfg.DBSSLMode,
	)
}

type Bot struct {
	configPath string
	config     *Config
	configMu   sync.RWMutex
	api        *tgbotapi.BotAPI
	updates    tgbotapi.UpdatesChannel
	logger     *log.Logger
	linkRepo   *link.Repository
	fileRepo   *repository.FileRepository
	adminMu    sync.RWMutex
	admins     map[int64]struct{}
}

func New(configPath string) (*Bot, error) {
	cfg, err := LoadConfig(configPath)
	if err != nil {
		return nil, err
	}

	db, err := openPostgres(cfg)
	if err != nil {
		return nil, err
	}

	api, err := tgbotapi.NewBotAPI(cfg.APIToken)
	if err != nil {
		return nil, err
	}
	api.Debug = false

	updateCfg := tgbotapi.NewUpdate(0)
	updateCfg.Timeout = 30
	updates := api.GetUpdatesChan(updateCfg)

	linkRepo := link.NewRepository(db)
	fileRepo := repository.NewFileRepository(db)

	return &Bot{
		configPath: configPath,
		config:     cfg,
		api:        api,
		updates:    updates,
		logger:     log.New(os.Stdout, "", log.LstdFlags),
		linkRepo:   linkRepo,
		fileRepo:   fileRepo,
		admins:     make(map[int64]struct{}),
	}, nil
}

func (b *Bot) Run(ctx context.Context) error {
	b.logger.Printf("Bot %s ready", b.getBotUsername())

	for {
		select {
		case <-ctx.Done():
			b.logger.Println("shutdown requested")
			return ctx.Err()
		case update, ok := <-b.updates:
			if !ok {
				return errors.New("updates channel closed")
			}
			if update.CallbackQuery != nil {
				b.handleCallbackQuery(update.CallbackQuery)
				continue
			}
			if update.Message == nil {
				continue
			}
			if update.Message.IsCommand() {
				b.handleCommand(update.Message)
				continue
			}
			if update.Message.Document != nil || update.Message.Video != nil || len(update.Message.Photo) > 0 {
				b.handleMedia(update.Message)
			}
		}
	}
}

func (b *Bot) handleCommand(message *tgbotapi.Message) {
	switch message.Command() {
	case "start":
		b.handleStart(message)
	case "help":
		b.handleHelp(message)
	case "login":
		b.handleLogin(message)
	case "logout":
		b.handleLogout(message)
	case "setcaption":
		b.handleSetCaption(message)
	case "settag":
		b.handleConfigUpdate(message, func(cfg *Config, args []string) (string, bool, error) {
			if len(args) != 1 || !strings.HasPrefix(args[0], "@") {
				return "Usage: /settag @new_tag", false, nil
			}
			cfg.DefaultTag = args[0]
			return fmt.Sprintf("Default tag updated to %s", cfg.DefaultTag), true, nil
		})
	}
}

func (b *Bot) parseArgs(text string) []string {
	return strings.Fields(text)
}

func (b *Bot) handleConfigUpdate(message *tgbotapi.Message, updater func(cfg *Config, args []string) (string, bool, error)) {
	if message.From == nil {
		return
	}
	if !b.isAdmin(message.From.ID) {
		return
	}
	args := b.parseArgs(message.CommandArguments())
	response, err := b.updateConfig(func(cfg *Config) (string, bool, error) {
		return updater(cfg, args)
	})
	if response != "" {
		b.reply(message.Chat.ID, response)
	}
	if err != nil {
		b.logger.Printf("failed to persist config: %v", err)
		b.reply(message.Chat.ID, "Failed to persist config")
	}
}

func (b *Bot) handleStart(message *tgbotapi.Message) {
	if message.From == nil {
		return
	}
	args := b.parseArgs(message.CommandArguments())
	if len(args) == 0 {
		msg := tgbotapi.NewMessage(message.Chat.ID, localization.WelcomeText+"\n\n"+guideShort)
		msg.ReplyMarkup = b.buildGuideKeyboard()
		if _, err := b.api.Send(msg); err != nil {
			b.logger.Printf("failed to send start message: %v", err)
		}
		return
	}
	if !b.isMember(message.From.ID) {
		keyboard := b.buildJoinKeyboard()
		msg := tgbotapi.NewMessage(message.Chat.ID, localization.JoinText)
		msg.ReplyMarkup = keyboard
		if _, err := b.api.Send(msg); err != nil {
			b.logger.Printf("failed to send join instructions: %v", err)
		}
		return
	}

	fileKey := args[0]
	record, err := b.getFile(fileKey)
	if err != nil {
		b.logger.Printf("failed to fetch file key %s: %v", fileKey, err)
		b.reply(message.Chat.ID, "مشکلی پیش آمد، لطفاً دوباره تلاش کنید.")
		return
	}
	if record == nil {
		b.reply(message.Chat.ID, localization.NotFoundText)
		return
	}
	if err := b.sendFileByType(message.Chat.ID, record); err != nil {
		b.logger.Printf("failed to send file %s: %v", fileKey, err)
	}
}

func (b *Bot) handleMedia(message *tgbotapi.Message) {
	if message.From == nil || !b.isAdmin(message.From.ID) {
		return
	}
	var fileID, fileType string
	switch {
	case message.Document != nil:
		fileID = message.Document.FileID
		fileType = "document"
	case message.Video != nil:
		fileID = message.Video.FileID
		fileType = "video"
	case len(message.Photo) > 0:
		fileID = message.Photo[len(message.Photo)-1].FileID
		fileType = "photo"
	default:
		return
	}
	caption := b.processCaption(message.Caption)
	fileKey, err := b.addFile(fileID, fileType, caption)
	if err != nil {
		b.logger.Printf("failed to save file: %v", err)
		return
	}
	linkURL := fmt.Sprintf("https://t.me/%s?start=%s", strings.TrimPrefix(b.getBotUsername(), "@"), fileKey)
	if err := b.linkRepo.Save(&link.Link{
		FileKey:   fileKey,
		URL:       linkURL,
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		b.logger.Printf("failed to save link for %s: %v", fileKey, err)
	}
	b.reply(message.Chat.ID, fmt.Sprintf("File link created:\n%s", linkURL))
	b.promptCaption(message.Chat.ID, fileKey, caption)
}

func (b *Bot) sendFileByType(chatID int64, record *repository.FileRecord) error {
	switch record.FileType {
	case "document":
		msg := tgbotapi.NewDocument(chatID, tgbotapi.FileID(record.FileID))
		msg.Caption = record.Caption
		_, err := b.api.Send(msg)
		return err
	case "photo":
		msg := tgbotapi.NewPhoto(chatID, tgbotapi.FileID(record.FileID))
		msg.Caption = record.Caption
		_, err := b.api.Send(msg)
		return err
	case "video":
		videoMsg := tgbotapi.NewVideo(chatID, tgbotapi.FileID(record.FileID))
		videoMsg.Caption = record.Caption
		sentVideo, err := b.api.Send(videoMsg)
		if err != nil {
			return err
		}
		warn := tgbotapi.NewMessage(chatID, localization.WarningText)
		sentWarn, err := b.api.Send(warn)
		if err != nil {
			return err
		}
		messageIDs := []int{sentVideo.MessageID, sentWarn.MessageID}
		b.deleteMessagesLater(chatID, messageIDs, time.Duration(b.getConfig().DeleteDelay)*time.Second)
		return nil
	default:
		return fmt.Errorf("unknown file type %s", record.FileType)
	}
}

func (b *Bot) deleteMessagesLater(chatID int64, messageIDs []int, delay time.Duration) {
	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
			for _, id := range messageIDs {
				if _, err := b.api.Request(tgbotapi.DeleteMessageConfig{
					ChatID:    chatID,
					MessageID: id,
				}); err != nil {
					b.logger.Printf("delete message %d failed: %v", id, err)
				}
			}
		}
	}()
}

func (b *Bot) addFile(fileID, fileType, caption string) (string, error) {
	if caption == "" {
		caption = b.getConfig().DefaultTag
	}
	if fileType == "" {
		fileType = "document"
	}
	return b.fileRepo.Save(fileID, fileType, caption)
}

func (b *Bot) updateCaption(fileKey, caption string) error {
	cleaned := b.processCaption(caption)
	return b.fileRepo.UpdateCaption(fileKey, cleaned)
}

func (b *Bot) getFile(fileKey string) (*repository.FileRecord, error) {
	return b.fileRepo.Get(fileKey)
}

func (b *Bot) processCaption(caption string) string {
	if caption == "" {
		return b.getConfig().DefaultTag
	}
	cleaned := mentionRe.ReplaceAllString(caption, b.getConfig().DefaultTag)
	if !strings.Contains(cleaned, b.getConfig().DefaultTag) {
		return b.getConfig().DefaultTag
	}
	return cleaned
}

func (b *Bot) isMember(userID int64) bool {
	channels := b.getConfig().SponsoredChannels
	if len(channels) == 0 {
		return true
	}
	for _, channel := range channels {
		if !b.userHasStatus(channel, userID) {
			return false
		}
	}
	return true
}

func (b *Bot) userHasStatus(channel string, userID int64) bool {
	normalized := normalizeChannel(channel)
	if normalized == "" {
		return false
	}

	chatConfig := tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID:             0,
			SuperGroupUsername: "@" + normalized,
			UserID:             userID,
		},
	}
	member, err := b.api.GetChatMember(chatConfig)
	if err != nil {
		b.logger.Printf("get chat member %s: %v", channel, err)
		return false
	}
	switch member.Status {
	case "member", "administrator", "creator":
		return true
	default:
		return false
	}
}

func normalizeChannel(channel string) string {
	channel = strings.TrimSpace(channel)
	channel = strings.TrimPrefix(channel, "@")
	channel = strings.TrimPrefix(channel, "https://t.me/")
	channel = strings.TrimPrefix(channel, "http://t.me/")
	channel = strings.TrimSuffix(channel, "/")
	return strings.TrimSpace(channel)
}

func (b *Bot) buildGuideKeyboard() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📤 How to upload", "guide_upload"),
			tgbotapi.NewInlineKeyboardButtonData("🔗 How to get link", "guide_link"),
		),
	)
}

func (b *Bot) buildJoinKeyboard() tgbotapi.InlineKeyboardMarkup {
	cfg := b.getConfig()

	rows := make([][]tgbotapi.InlineKeyboardButton, 0, len(cfg.SponsoredChannels))
	for _, sponsor := range cfg.SponsoredChannels {
		sponsor = strings.TrimSpace(sponsor)
		if sponsor == "" {
			continue
		}
		target := normalizeChannel(sponsor)
		if target == "" {
			continue
		}
		rows = append(rows, []tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardButtonURL(sponsor, fmt.Sprintf("https://t.me/%s", target)),
		})
	}

	if len(rows) == 0 {
		return tgbotapi.NewInlineKeyboardMarkup()
	}

	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

func (b *Bot) handleCallbackQuery(cq *tgbotapi.CallbackQuery) {
	if cq.Message == nil || cq.Data == "" {
		return
	}
	callback := tgbotapi.NewCallback(cq.ID, "")
	if _, err := b.api.Request(callback); err != nil {
		b.logger.Printf("answer callback: %v", err)
	}
	var text string
	switch cq.Data {
	case "guide_upload":
		text = guideUpload
	case "guide_link":
		text = guideGetLink
	default:
		return
	}
	msg := tgbotapi.NewMessage(cq.Message.Chat.ID, text)
	msg.ParseMode = "Markdown"
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Printf("send guide: %v", err)
	}
}

func (b *Bot) handleHelp(message *tgbotapi.Message) {
	if message.From == nil {
		return
	}
	full := guideShort + "\n\n---\n\n" + guideUpload + "\n\n---\n\n" + guideGetLink
	msg := tgbotapi.NewMessage(message.Chat.ID, full)
	msg.ParseMode = "Markdown"
	msg.ReplyMarkup = b.buildGuideKeyboard()
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Printf("send help: %v", err)
	}
}

func (b *Bot) promptCaption(chatID int64, fileKey, caption string) {
	msg := fmt.Sprintf("Caption saved as:\n%s\nIf you'd like to change it before users open the link, send:\n/setcaption %s <new caption>", caption, fileKey)
	b.reply(chatID, msg)
}

func (b *Bot) handleSetCaption(message *tgbotapi.Message) {
	if message.From == nil || !b.isAdmin(message.From.ID) {
		return
	}
	raw := strings.TrimLeft(message.CommandArguments(), " \t")
	if raw == "" {
		b.reply(message.Chat.ID, "Usage: /setcaption <file_key> <new caption>")
		return
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		b.reply(message.Chat.ID, "Usage: /setcaption <file_key> <new caption>")
		return
	}
	fileKey := fields[0]
	caption := raw[len(fileKey):]
	caption = strings.TrimLeft(caption, " \t")
	if strings.TrimSpace(caption) == "" {
		b.reply(message.Chat.ID, "Caption cannot be empty.")
		return
	}
	if err := b.updateCaption(fileKey, caption); err != nil {
		b.logger.Printf("update caption failed for %s: %v", fileKey, err)
		b.reply(message.Chat.ID, "Failed to update caption.")
		return
	}
	b.reply(message.Chat.ID, fmt.Sprintf("Caption for %s updated.", fileKey))
}

func (b *Bot) handleLogin(message *tgbotapi.Message) {
	if message.From == nil {
		return
	}
	args := b.parseArgs(message.CommandArguments())
	if len(args) != 1 {
		b.reply(message.Chat.ID, "Usage: /login <password>")
		return
	}
	if args[0] != b.getConfig().AdminPassword {
		b.reply(message.Chat.ID, "Invalid password.")
		return
	}
	b.setAdmin(message.From.ID, true)
	b.reply(message.Chat.ID, "You are now authenticated as admin. You can upload videos and run admin commands.")
}

func (b *Bot) handleLogout(message *tgbotapi.Message) {
	if message.From == nil {
		return
	}
	if !b.isAdmin(message.From.ID) {
		b.reply(message.Chat.ID, "You are not logged in.")
		return
	}
	b.setAdmin(message.From.ID, false)
	b.reply(message.Chat.ID, "Logged out from admin mode.")
}

func (b *Bot) reply(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Printf("reply failed: %v", err)
	}
}

func (b *Bot) isAdmin(userID int64) bool {
	b.adminMu.RLock()
	defer b.adminMu.RUnlock()
	_, ok := b.admins[userID]
	return ok
}

func (b *Bot) setAdmin(userID int64, active bool) {
	b.adminMu.Lock()
	defer b.adminMu.Unlock()
	if active {
		b.admins[userID] = struct{}{}
		return
	}
	delete(b.admins, userID)
}

func (b *Bot) getConfig() *Config {
	b.configMu.RLock()
	defer b.configMu.RUnlock()
	return b.config
}

func (b *Bot) updateConfig(updater func(cfg *Config) (string, bool, error)) (string, error) {
	b.configMu.Lock()
	defer b.configMu.Unlock()
	response, dirty, err := updater(b.config)
	if err != nil {
		return response, err
	}
	if dirty {
		if err := b.persistConfig(); err != nil {
			return response, err
		}
	}
	return response, nil
}

func (b *Bot) persistConfig() error {
	raw, err := yaml.Marshal(b.config)
	if err != nil {
		return err
	}
	return os.WriteFile(b.configPath, raw, 0o600)
}

func initDB(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS files (
			id SERIAL PRIMARY KEY,
			file_id TEXT NOT NULL,
			file_key TEXT NOT NULL UNIQUE,
			caption TEXT,
			file_type TEXT NOT NULL
		);
	`)
	if err != nil {
		return err
	}
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS links (
			id SERIAL PRIMARY KEY,
			file_key TEXT NOT NULL UNIQUE,
			url TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL
		);
	`)
	return err
}

func openPostgres(cfg *Config) (*sql.DB, error) {
	dsn := cfg.databaseDSN()
	var db *sql.DB
	var err error
	for i := 0; i < 15; i++ {
		db, err = sql.Open("postgres", dsn)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			if initErr := initDB(db); initErr != nil {
				return nil, initErr
			}
			return db, nil
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("postgres connect: %w", err)
}

func (b *Bot) getBotUsername() string {
	return b.getConfig().BotUsername
}
