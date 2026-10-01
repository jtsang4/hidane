// Package feishu binds the Feishu (Lark) channel: a p2p chat's top level is
// the main thread; the reply thread under a bot-posted "📋 wi_x — title" root
// is that work item's thread. Inbound events arrive over the official SDK's
// long connection — the desktop app has no public callback URL, and the
// connection is authenticated with the app credentials, so there is no open
// endpoint to verify. Answers leave through an outbox consumer on its own
// cursor, so a reply reaches Feishu whichever process wrote it.
package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/kernel"
)

// Messenger is the Feishu API surface hidane uses.
type Messenger interface {
	// SendText posts to a chat; rich sends a markdown card, otherwise plain text.
	SendText(ctx context.Context, chatID, text string, rich bool) (string, error)
	ReplyInThread(ctx context.Context, rootMessageID, text string) (string, error)
	// FetchImage downloads a message image.
	FetchImage(ctx context.Context, messageID, key string) ([]byte, string, error)
}

// Inbound is a received message, normalized.
type Inbound struct {
	EventID      string
	SenderType   string
	SenderOpenID string
	MessageID    string
	ChatID       string
	ChatType     string
	MessageType  string
	RootID       string
	Content      string
}

type Channel struct {
	K   *kernel.Kernel
	Sys *agents.System
	M   Messenger
	// Allowed are the open_ids whose messages reach the agents. Empty means
	// trust on first use: the first private chat becomes the owner's.
	Allowed []string

	mu   sync.Mutex
	seen map[string]bool
	ring []string
}

func New(k *kernel.Kernel, sys *agents.System, m Messenger) *Channel {
	return &Channel{K: k, Sys: sys, M: m, seen: map[string]bool{}}
}

var codeFence = regexp.MustCompile("(?s)```[^\\n]*\\n(.*?)```")

// FlattenCodeFences: fenced code is the one construct Feishu cards render in
// no version, so the markers would show literally. Indented, it at least
// stays visually separate.
func FlattenCodeFences(text string) string {
	return codeFence.ReplaceAllStringFunc(text, func(m string) string {
		body := codeFence.FindStringSubmatch(m)[1]
		body = strings.TrimSuffix(body, "\n")
		lines := strings.Split(body, "\n")
		for i, l := range lines {
			lines[i] = "    " + l
		}
		return strings.Join(lines, "\n")
	})
}

// MarkdownCard is a card in JSON 2.0: 1.0 renders only a subset of markdown
// and leaves lists and tables as literal text.
func MarkdownCard(text string) string {
	b, _ := json.Marshal(map[string]any{
		"schema": "2.0",
		"config": map[string]any{"width_mode": "fill", "update_multi": true},
		"body":   map[string]any{"elements": []any{map[string]any{"tag": "markdown", "content": FlattenCodeFences(text)}}},
	})
	return string(b)
}

const chunkLimit = 3500

// ChunkText splits a long reply instead of cutting it off — a truncated
// answer once read as the runtime hanging. Blank lines first, then lines, and
// only a single oversized line is broken mid-line.
func ChunkText(text string, limit int) []string {
	if limit <= 0 {
		limit = chunkLimit
	}
	runes := func(s string) int { return len([]rune(s)) }
	if runes(text) <= limit {
		if text == "" {
			return nil
		}
		return []string{text}
	}
	var chunks []string
	current := ""
	flush := func() {
		if current != "" {
			chunks = append(chunks, current)
		}
		current = ""
	}
	push := func(piece, sep string) {
		if current == "" && runes(piece) <= limit {
			current = piece
			return
		}
		if runes(current)+runes(sep)+runes(piece) <= limit {
			current += sep + piece
			return
		}
		flush()
		if runes(piece) <= limit {
			current = piece
			return
		}
		r := []rune(piece)
		for i := 0; i < len(r); i += limit {
			end := i + limit
			if end > len(r) {
				end = len(r)
			}
			chunks = append(chunks, string(r[i:end]))
		}
	}
	for _, block := range strings.Split(text, "\n\n") {
		if runes(block) <= limit {
			push(block, "\n\n")
			continue
		}
		for _, line := range strings.Split(block, "\n") {
			push(line, "\n")
		}
	}
	flush()
	return chunks
}

// labelled numbers the parts so several messages read as one answer.
func labelled(chunks []string) []string {
	if len(chunks) <= 1 {
		return chunks
	}
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = fmt.Sprintf("%s\n\n（%d/%d）", c, i+1, len(chunks))
	}
	return out
}

var atUser = regexp.MustCompile(`@_user_\d+\s*`)

// ExtractText reads a text message's words without @-mentions.
func ExtractText(content string) string {
	var parsed struct {
		Text string `json:"text"`
	}
	if json.Unmarshal([]byte(content), &parsed) != nil {
		return ""
	}
	return strings.TrimSpace(atUser.ReplaceAllString(parsed.Text, ""))
}

// ImageKeys lists the images a message carries (image, or post with embedded images).
func ImageKeys(messageType, content string) []string {
	var parsed map[string]any
	if json.Unmarshal([]byte(content), &parsed) != nil {
		return nil
	}
	switch messageType {
	case "image":
		if k, ok := parsed["image_key"].(string); ok {
			return []string{k}
		}
	case "post":
		var keys []string
		rows, _ := parsed["content"].([]any)
		for _, row := range rows {
			els, _ := row.([]any)
			for _, el := range els {
				m, _ := el.(map[string]any)
				if m["tag"] == "img" {
					if k, ok := m["image_key"].(string); ok {
						keys = append(keys, k)
					}
				}
			}
		}
		return keys
	}
	return nil
}

// SniffMime reads the content type from magic bytes: download headers are not
// reliable, and a jpeg labelled png is rejected by vision models.
func SniffMime(b []byte, fallback string) string {
	switch {
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return "image/jpeg"
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(b) >= 6 && strings.HasPrefix(string(b[:6]), "GIF8"):
		return "image/gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return fallback
}

// ImageOnlyText stands in for words on an image-only message.
const ImageOnlyText = "(图片消息，请查看附带图片)"

// DescribeMessage tells the agent what actually arrived when there are no
// words — truthfully: a failed download says so rather than pretending.
func DescribeMessage(messageType string, keys []string, images int, failures int) string {
	if images > 0 {
		if failures > 0 {
			return fmt.Sprintf("(图片消息：附带 %d 张图片，另有 %d 张下载失败)", images, failures)
		}
		return ImageOnlyText
	}
	if len(keys) > 0 {
		return fmt.Sprintf("(收到 %d 张图片，但下载失败，无法查看内容)", len(keys))
	}
	if messageType == "" {
		messageType = "unknown"
	}
	return fmt.Sprintf("(收到一条 %s 类型的消息，暂不支持解析内容)", messageType)
}

// duplicate is the at-most-once gate on event ids: the platform redelivers
// until acknowledged, and a redelivery would run the whole chain again.
func (c *Channel) duplicate(id string) bool {
	if id == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[id] {
		return true
	}
	c.seen[id] = true
	c.ring = append(c.ring, id)
	if len(c.ring) > 2000 {
		for _, old := range c.ring[:1000] {
			delete(c.seen, old)
		}
		c.ring = c.ring[1000:]
	}
	return false
}

// allowed decides who may drive the agents. Everyone who can message the bot
// is not someone the agents should run code for: with no allowlist, only the
// first private chat (the owner's) is heard, and group chats are not.
func (c *Channel) allowed(ctx context.Context, in Inbound) (bool, error) {
	if len(c.Allowed) > 0 {
		for _, id := range c.Allowed {
			if id == in.SenderOpenID {
				return true, nil
			}
		}
		return false, nil
	}
	main, ok, err := c.K.FindMainBinding(ctx, "feishu")
	if err != nil {
		return false, err
	}
	if ok {
		return in.ChatID == main.ChatID, nil
	}
	return in.ChatType == "p2p", nil
}

// HandleMessage records an inbound message and hands it to the one message door.
func (c *Channel) HandleMessage(ctx context.Context, in Inbound) error {
	if in.SenderType != "user" || in.ChatID == "" { // bot echoes never re-enter
		return nil
	}
	if c.duplicate(in.EventID) {
		return nil
	}
	k := c.K
	// Durable half of the retry guard: the in-memory set does not survive a restart.
	if in.MessageID != "" {
		prior, err := k.ListEvents(ctx, kernel.ListFilter{Kind: "connector.feishu", PayloadKey: "messageId", PayloadValue: in.MessageID, Tail: 1})
		if err != nil {
			return err
		}
		if len(prior) > 0 {
			return nil
		}
	}
	allowed, err := c.allowed(ctx, in)
	if err != nil {
		return err
	}
	keys := ImageKeys(in.MessageType, in.Content)
	var images []agents.InboundImage
	var failures []string
	for i, key := range keys {
		// Nothing is fetched for a sender the agents will not act for.
		if i == 4 || !allowed {
			break
		}
		b, header, err := c.M.FetchImage(ctx, in.MessageID, key)
		if err == nil && len(b) == 0 {
			err = fmt.Errorf("empty body")
		}
		if err != nil {
			// A failed image must not sink the message — but it must be visible.
			failures = append(failures, key+": "+err.Error())
			continue
		}
		fallback := "image/png"
		if strings.HasPrefix(header, "image/") {
			fallback = header
		}
		images = append(images, agents.InboundImage{Data: encodeBase64(b), MimeType: SniffMime(b, fallback)})
	}
	words := ExtractText(in.Content)
	text := words
	if text == "" {
		text = DescribeMessage(in.MessageType, keys, len(images), len(failures))
	}
	payload := kernel.Payload{"chatId": in.ChatID, "rootId": nilIfEmpty(in.RootID), "messageId": nilIfEmpty(in.MessageID),
		"messageType": in.MessageType, "imageCount": len(images), "text": clip(text, 2000)}
	if len(failures) > 0 {
		payload["imageFailures"] = failures
	}
	// Connectors capture, never judge: everything is recorded first.
	if _, err := k.Append(ctx, kernel.EventInput{Source: "connector:feishu", Kind: "connector.feishu", Payload: payload}); err != nil {
		return err
	}
	if len(failures) > 0 {
		_, _ = k.Append(ctx, kernel.EventInput{Source: "connector:feishu", Kind: "agent.error",
			Payload: kernel.Payload{"error": fmt.Sprintf("failed to download %d image(s) from Feishu", len(failures)), "detail": failures, "messageId": nilIfEmpty(in.MessageID)}})
	}
	// The log takes everything; a model wakes only for input it can act on,
	// from someone allowed to ask.
	if words == "" && len(keys) == 0 {
		return nil
	}
	if !allowed {
		_, err := k.Append(ctx, kernel.EventInput{Source: "connector:feishu", Kind: "connector.feishu_ignored",
			Payload: kernel.Payload{"chatId": in.ChatID, "chatType": in.ChatType, "messageId": nilIfEmpty(in.MessageID),
				"reason": "sender is not allowed to drive the agents (set feishu.allowedUsers)"}})
		return err
	}
	// The p2p chat is the main-thread binding: a scheduled reminder cannot
	// reach a chat nobody recorded.
	if in.ChatType == "p2p" {
		if _, ok, err := k.FindByChannelRef(ctx, "feishu", in.ChatID, ""); err == nil && !ok {
			if _, err := k.CreateBinding(ctx, kernel.ChannelBinding{Channel: "feishu", Kind: "main", ChatID: in.ChatID}); err != nil {
				return err
			}
		}
	}
	target := ""
	if in.RootID != "" {
		if b, ok, err := k.FindByChannelRef(ctx, "feishu", in.ChatID, in.RootID); err == nil && ok && b.Kind == "work_item" {
			target = b.WorkItemID
		}
	}
	_, err = c.Sys.SubmitMessage(ctx, agents.InboundMessage{
		Text: text, Images: images, Source: "connector:feishu", Target: target,
		Channel: map[string]any{"feishu": map[string]any{"chatId": in.ChatID, "rootId": nilIfEmpty(in.RootID), "messageId": nilIfEmpty(in.MessageID)}},
	})
	return err
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// OutboundText renders one runtime answer for Feishu.
func OutboundText(e kernel.Event) string {
	p := e.Payload
	switch e.Kind {
	case "agent.reply":
		return p.Str("text")
	case "escalation":
		var trail []string
		if path, ok := p["path"].([]any); ok {
			for _, step := range path {
				s, _ := step.(map[string]any)
				if tried, _ := s["tried"].(string); tried != "" {
					title, _ := s["title"].(string)
					trail = append(trail, "- "+title+"："+tried)
				}
			}
		}
		out := "❓ " + p.Str("question")
		if len(trail) > 0 {
			out += "\n\n已经尝试过：\n" + strings.Join(trail, "\n")
		}
		return out
	case "attribution.ambiguous":
		lines := []string{p.Str("question")}
		if list, ok := p["candidates"].([]any); ok {
			for _, c := range list {
				m, _ := c.(map[string]any)
				if m["workItemId"] == "new" {
					lines = append(lines, "- 新任务")
				} else {
					lines = append(lines, fmt.Sprintf("- %v（%v）", m["title"], m["workItemId"]))
				}
			}
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

func (c *Channel) sendChunked(ctx context.Context, chatID, text string) error {
	for _, part := range labelled(ChunkText(text, chunkLimit)) {
		if _, err := c.M.SendText(ctx, chatID, part, true); err != nil {
			return err
		}
	}
	return nil
}

func (c *Channel) replyChunked(ctx context.Context, root, text string) error {
	for _, part := range labelled(ChunkText(text, chunkLimit)) {
		if _, err := c.M.ReplyInThread(ctx, root, part); err != nil {
			return err
		}
	}
	return nil
}

// workItemRoot is the work item's Feishu thread root, created on first use.
func (c *Channel) workItemRoot(ctx context.Context, workItemID, chatID string) (string, error) {
	if b, ok, err := c.K.FindByWorkItem(ctx, "feishu", workItemID); err != nil || ok {
		return b.RootID, err
	}
	item, err := c.K.GetWorkItem(ctx, workItemID)
	if err != nil {
		return "", err
	}
	// A label, not prose: a plain-text root can be replied to in-thread.
	root, err := c.M.SendText(ctx, chatID, fmt.Sprintf("📋 %s — %s", item.ID, item.Title), false)
	if err != nil {
		return "", err
	}
	if _, err := c.K.CreateBinding(ctx, kernel.ChannelBinding{Channel: "feishu", Kind: "work_item", WorkItemID: item.ID, ChatID: chatID, RootID: root}); err != nil {
		return "", err
	}
	_, err = c.K.Append(ctx, kernel.EventInput{Source: "connector:feishu", Kind: "binding.created", ThreadID: item.ThreadID, WorkItemID: item.ID,
		Payload: kernel.Payload{"chatId": chatID, "rootId": root}})
	return root, err
}

// deliver sends one answer when the conversation it answers began on Feishu,
// or is a scheduled prompt (which has no inbound chat). Answers about a work
// item go into that item's thread.
func (c *Channel) deliver(ctx context.Context, e kernel.Event) error {
	text := OutboundText(e)
	if text == "" {
		return nil
	}
	rootID := e.Payload.Str("root")
	if rootID == "" {
		return nil
	}
	root, ok, err := c.K.GetEvent(ctx, rootID)
	if err != nil || !ok {
		return err
	}
	if root.Kind == "schedule.prompt" {
		b, ok, err := c.K.FindMainBinding(ctx, "feishu")
		if err != nil || !ok {
			return err
		}
		return c.sendChunked(ctx, b.ChatID, "⏰ "+root.Payload.Str("name")+"\n"+text)
	}
	channel, _ := root.Payload["channel"].(map[string]any)
	origin, _ := channel["feishu"].(map[string]any)
	chatID, _ := origin["chatId"].(string)
	if chatID == "" {
		return nil
	}
	if e.WorkItemID != "" {
		threadRoot, err := c.workItemRoot(ctx, e.WorkItemID, chatID)
		if err != nil {
			return err
		}
		if threadRoot != "" {
			return c.replyChunked(ctx, threadRoot, text)
		}
	}
	return c.sendChunked(ctx, chatID, text)
}

const outbox = "feishu-outbox"

var outboundKinds = []string{"agent.reply", "escalation", "attribution.ambiguous"}

// OutboxOnce runs one pass of the outbox consumer. A fresh install starts at
// the tail: history is never re-sent. Delivery failures are recorded and skipped.
func (c *Channel) OutboxOnce(ctx context.Context) (int, error) {
	k := c.K
	var exists int
	if err := k.DB.QueryRowContext(ctx, `SELECT count(*) FROM cursors WHERE consumer = ?`, outbox).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		last, err := k.LatestSeq(ctx)
		if err != nil {
			return 0, err
		}
		return 0, k.CommitCursor(ctx, outbox, last)
	}
	after, err := k.GetCursor(ctx, outbox)
	if err != nil {
		return 0, err
	}
	batch, err := k.ListEvents(ctx, kernel.ListFilter{AfterSeq: &after, Kinds: outboundKinds, Limit: 50})
	if err != nil {
		return 0, err
	}
	for _, e := range batch {
		if err := c.deliver(ctx, e); err != nil {
			_, _ = k.Append(ctx, kernel.EventInput{Source: "connector:feishu", Kind: "agent.error",
				Payload: kernel.Payload{"error": "feishu delivery failed: " + err.Error(), "of": e.ID}})
		}
		if err := k.CommitCursor(ctx, outbox, e.Seq); err != nil {
			return 0, err
		}
	}
	return len(batch), nil
}

// RunOutbox consumes on every append and on a fallback poll.
func (c *Channel) RunOutbox(ctx context.Context) {
	wake, cancel := c.K.Hub.Subscribe()
	defer cancel()
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	run := func() {
		for {
			n, err := c.OutboxOnce(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("feishu outbox: %v", err)
				}
				return
			}
			if n < 50 {
				return
			}
		}
	}
	run()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			run()
		case <-t.C:
			run()
		}
	}
}

func kernelError(err error) kernel.EventInput {
	return kernel.EventInput{Source: "connector:feishu", Kind: "agent.error", Payload: kernel.Payload{"error": err.Error()}}
}
