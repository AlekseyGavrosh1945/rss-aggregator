// Package telegram implements the Telegram bot: user commands
// and delivery of new feed posts to chats.
package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	tg "gopkg.in/telebot.v3"

	"github.com/AlekseyGavrosh1945/rss-aggregator/internal/storage"
)

// cbUnsub is the unique callback id of the "unsubscribe" inline button;
// the button data carries the feed id.
const cbUnsub = "unsub"

const helpText = `Я RSS-агрегатор 📡

Команды:
/add <ссылка> — подписаться на RSS/Atom-фид (можно просто адрес сайта — ленту найду сам)
/list — показать подписки, отписаться можно кнопкой
/remove <ссылка> — отписаться текстом
/help — справка

Новые посты из ваших фидов буду присылать сюда автоматически.`

// FeedResolver maps a user-supplied URL to an actual feed URL
// (implemented by feed.Fetcher).
type FeedResolver interface {
	ResolveFeedURL(ctx context.Context, rawURL string) (string, error)
}

// Bot wraps the telebot client, handles commands and sends notifications.
type Bot struct {
	bot      *tg.Bot
	store    *storage.Store
	resolver FeedResolver
	log      *slog.Logger
}

// New creates the bot. An empty token returns (nil, nil): the bot is
// disabled and the app keeps working in fetch-only mode.
func New(token string, store *storage.Store, resolver FeedResolver, log *slog.Logger) (*Bot, error) {
	if token == "" {
		return nil, nil
	}
	b, err := tg.NewBot(tg.Settings{
		Token:  token,
		Poller: &tg.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		return nil, fmt.Errorf("create bot: %w", err)
	}

	bot := &Bot{bot: b, store: store, resolver: resolver, log: log}
	b.Handle("/start", bot.onStart)
	b.Handle("/help", bot.onHelp)
	b.Handle("/add", bot.onAdd)
	b.Handle("/list", bot.onList)
	b.Handle("/remove", bot.onRemove)
	// Callback endpoints are dispatched as "\f<unique>" (see telebot.ProcessUpdate).
	b.Handle("\f"+cbUnsub, bot.onUnsubscribe)
	return bot, nil
}

// Start runs the long-polling loop; it blocks until Stop is called.
func (b *Bot) Start() { b.bot.Start() }

// Stop stops the polling loop.
func (b *Bot) Stop() { b.bot.Stop() }

func (b *Bot) onStart(c tg.Context) error {
	if _, err := b.store.EnsureUser(context.Background(), c.Sender().ID); err != nil {
		b.log.Error("ensure user", "err", err)
	}
	return c.Send(helpText)
}

func (b *Bot) onHelp(c tg.Context) error { return c.Send(helpText) }

func (b *Bot) onAdd(c tg.Context) error {
	raw := strings.TrimSpace(c.Message().Payload)
	if raw == "" {
		return c.Send("Использование: /add <ссылка на сайт или RSS-фид>")
	}
	if !isHTTPURL(raw) {
		return c.Send("Это не похоже на ссылку. Нужен URL вида https://example.com")
	}

	ctx := context.Background()
	resolved, err := b.resolver.ResolveFeedURL(ctx, raw)
	if err != nil {
		b.log.Warn("resolve feed", "url", raw, "err", err)
		return c.Send("Не смог найти RSS-ленту по этой ссылке. " +
			"Попробуйте прямую ссылку на фид — обычно она заканчивается на /feed или /rss.")
	}

	userID, err := b.store.EnsureUser(ctx, c.Sender().ID)
	if err != nil {
		b.log.Error("ensure user", "err", err)
		return c.Send("Не получилось сохранить подписку, попробуйте позже.")
	}

	f, isNew, err := b.store.Subscribe(ctx, userID, resolved)
	if err != nil {
		b.log.Error("subscribe", "err", err)
		return c.Send("Не получилось сохранить подписку, попробуйте позже.")
	}
	if !isNew {
		return c.Send("Вы уже подписаны на " + displayTitle(f))
	}
	return c.Send("Подписка оформлена: " + displayTitle(f) +
		"\nЛента: " + f.URL +
		"\nНовые посты будут приходить сюда автоматически.")
}

func (b *Bot) onList(c tg.Context) error {
	userID, err := b.store.EnsureUser(context.Background(), c.Sender().ID)
	if err != nil {
		b.log.Error("ensure user", "err", err)
		return c.Send("Что-то сломалось, попробуйте позже.")
	}

	feeds, err := b.store.UserFeeds(context.Background(), userID)
	if err != nil {
		b.log.Error("list feeds", "err", err)
		return c.Send("Что-то сломалось, попробуйте позже.")
	}
	if len(feeds) == 0 {
		return c.Send("Пока нет подписок. Добавьте первую: /add <ссылка на сайт или фид>")
	}

	text, markup := renderSubscriptions(feeds)
	return c.Send(text, markup)
}

// onUnsubscribe handles clicks on the "unsubscribe" inline button:
// it removes the subscription and re-renders the list message.
func (b *Bot) onUnsubscribe(c tg.Context) error {
	feedID, err := strconv.ParseInt(c.Data(), 10, 64)
	if err != nil {
		return c.Respond(&tg.CallbackResponse{Text: "Некорректная кнопка"})
	}

	ctx := context.Background()
	userID, err := b.store.EnsureUser(ctx, c.Sender().ID)
	if err != nil {
		b.log.Error("ensure user", "err", err)
		return c.Respond(&tg.CallbackResponse{Text: "Ошибка, попробуйте позже"})
	}

	removed, err := b.store.UnsubscribeByID(ctx, userID, feedID)
	if err != nil {
		b.log.Error("unsubscribe", "err", err)
		return c.Respond(&tg.CallbackResponse{Text: "Ошибка, попробуйте позже"})
	}
	if !removed {
		return c.Respond(&tg.CallbackResponse{Text: "Вы уже отписаны от этой ленты"})
	}

	feeds, err := b.store.UserFeeds(ctx, userID)
	if err != nil {
		b.log.Error("list feeds", "err", err)
		return c.Respond(&tg.CallbackResponse{Text: "Отписался, но список не обновился"})
	}
	if len(feeds) == 0 {
		if err := c.Edit("Пока нет подписок. Добавьте первую: /add <ссылка на сайт или фид>"); err != nil {
			b.log.Warn("edit message", "err", err)
		}
	} else {
		text, markup := renderSubscriptions(feeds)
		if err := c.Edit(text, markup); err != nil {
			b.log.Warn("edit message", "err", err)
		}
	}
	return c.Respond(&tg.CallbackResponse{Text: "Отписался 👌"})
}

// renderSubscriptions builds the subscription list text and an inline
// keyboard with an "unsubscribe" button per feed.
func renderSubscriptions(feeds []storage.Feed) (string, *tg.ReplyMarkup) {
	var sb strings.Builder
	sb.WriteString("Ваши подписки (кнопка отписывает):\n")

	markup := &tg.ReplyMarkup{}
	rows := make([]tg.Row, 0, len(feeds))
	for i, f := range feeds {
		status := ""
		if f.LastError != "" {
			status = " ⚠️"
		}
		title := displayTitle(f)
		fmt.Fprintf(&sb, "%d. %s%s\n", i+1, title, status)
		btn := markup.Data("❌ "+truncateRunes(title, 30), cbUnsub, strconv.FormatInt(f.ID, 10))
		rows = append(rows, markup.Row(btn))
	}
	markup.Inline(rows...)
	return sb.String(), markup
}

func (b *Bot) onRemove(c tg.Context) error {
	feedURL := strings.TrimSpace(c.Message().Payload)
	if feedURL == "" {
		return c.Send("Использование: /remove <ссылка на RSS-фид>")
	}

	userID, err := b.store.EnsureUser(context.Background(), c.Sender().ID)
	if err != nil {
		b.log.Error("ensure user", "err", err)
		return c.Send("Что-то сломалось, попробуйте позже.")
	}

	removed, err := b.store.Unsubscribe(context.Background(), userID, feedURL)
	if err != nil {
		b.log.Error("unsubscribe", "err", err)
		return c.Send("Что-то сломалось, попробуйте позже.")
	}
	if !removed {
		return c.Send("Такой подписки нет. Посмотрите список: /list")
	}
	return c.Send("Вы отписались от " + feedURL)
}

// NotifyNewPosts implements scheduler.Notifier: it sends new posts to a chat.
func (b *Bot) NotifyNewPosts(_ context.Context, chatID int64, feedTitle string, posts []storage.Post) error {
	const maxPosts = 10

	var sb strings.Builder
	if feedTitle != "" {
		fmt.Fprintf(&sb, "🆕 <b>%s</b>\n\n", escapeHTML(feedTitle))
	}
	shown := posts
	if len(shown) > maxPosts {
		shown = shown[:maxPosts]
	}
	for _, p := range shown {
		title := p.Title
		if title == "" {
			title = p.Link
		}
		fmt.Fprintf(&sb, "• <a href=%q>%s</a>\n", escapeAttr(p.Link), escapeHTML(title))
	}
	if rest := len(posts) - len(shown); rest > 0 {
		fmt.Fprintf(&sb, "\n…и ещё %d", rest)
	}

	_, err := b.bot.Send(tg.ChatID(chatID), sb.String(), tg.ModeHTML)
	return err
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func displayTitle(f storage.Feed) string {
	if f.Title == "" || f.Title == f.URL {
		return f.URL
	}
	return f.Title
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max-1]) + "…"
}

var htmlEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

func escapeHTML(s string) string { return htmlEscaper.Replace(s) }

var attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")

func escapeAttr(s string) string { return attrEscaper.Replace(s) }
