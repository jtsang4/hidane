package feishu_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jtsang4/hidane/internal/agentcli"
	"github.com/jtsang4/hidane/internal/agents"
	"github.com/jtsang4/hidane/internal/feishu"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/kernel/kerneltest"
	"github.com/jtsang4/hidane/internal/settings"
)

var ctx = context.Background()

type sent struct {
	kind, target, text string
	rich               bool
}

type fakeMessenger struct {
	mu     sync.Mutex
	sent   []sent
	images map[string][]byte
	n      int
}

func (f *fakeMessenger) SendText(_ context.Context, chatID, text string, rich bool) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.sent = append(f.sent, sent{"send", chatID, text, rich})
	return fmt.Sprintf("om_%d", f.n), nil
}

func (f *fakeMessenger) ReplyInThread(_ context.Context, root, text string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	f.sent = append(f.sent, sent{"reply", root, text, true})
	return fmt.Sprintf("om_%d", f.n), nil
}

func (f *fakeMessenger) FetchImage(_ context.Context, _, key string) ([]byte, string, error) {
	if b, ok := f.images[key]; ok {
		return b, "application/octet-stream", nil
	}
	return nil, "", fmt.Errorf("feishu 234003: File not in msg")
}

type noAgents struct{}

func (noAgents) Start(context.Context, string, agentcli.Request) (agentcli.Run, error) {
	return nil, fmt.Errorf("unused")
}

func setup(t *testing.T) (*kernel.Kernel, *feishu.Channel, *fakeMessenger) {
	k := kerneltest.New(t)
	st, _ := settings.Load(k.Cfg.SettingsPath())
	sys := agents.New(k, st, noAgents{})
	fm := &fakeMessenger{images: map[string][]byte{"img_ok": []byte("\x89PNG\r\n\x1a\nrest")}}
	return k, feishu.New(k, sys, fm), fm
}

func m[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func text(s string) string { return fmt.Sprintf(`{"text":%q}`, s) }

func TestInboundP2PReachesThePrimaryOnce(t *testing.T) {
	k, ch, _ := setup(t)
	in := feishu.Inbound{EventID: "e1", SenderType: "user", SenderOpenID: "ou_owner", MessageID: "om_in1", ChatID: "oc_1", ChatType: "p2p", MessageType: "text", Content: text("@_user_1 帮我查一下")}
	if err := ch.HandleMessage(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := ch.HandleMessage(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.EventID = "e1-retry-after-restart"
	if err := ch.HandleMessage(ctx, in); err != nil {
		t.Fatal(err)
	}
	pending := m(k.PendingMessages(ctx, kernel.Primary, 10))
	if len(pending) != 1 || pending[0].Payload.Str("text") != "帮我查一下" {
		t.Fatalf("one delivery, mention stripped: %+v", pending)
	}
	channel := pending[0].Payload["channel"].(map[string]any)["feishu"].(map[string]any)
	if channel["chatId"] != "oc_1" {
		t.Fatalf("channel coordinates: %+v", channel)
	}
	if _, ok, _ := k.FindMainBinding(ctx, "feishu"); !ok {
		t.Fatal("the p2p chat becomes the main binding")
	}
	if n := len(m(k.ListEvents(ctx, kernel.ListFilter{Kind: "connector.feishu"}))); n != 1 {
		t.Fatalf("captured once: %d", n)
	}
}

func TestBotsAndStickersDoNotWakeAModel(t *testing.T) {
	k, ch, _ := setup(t)
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "bot", ChatID: "oc_1", MessageType: "text", Content: text("echo")})
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", MessageID: "om_s", ChatID: "oc_1", MessageType: "sticker", Content: `{"file_key":"x"}`})
	captured := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "connector.feishu"}))
	if len(captured) != 1 || !strings.Contains(captured[0].Payload.Str("text"), "sticker") {
		t.Fatalf("the sticker is recorded honestly, the bot echo not at all: %+v", captured)
	}
	if p := m(k.PendingMessages(ctx, kernel.Primary, 10)); len(p) != 0 {
		t.Fatal("nothing to act on")
	}
}

func TestImagesAreDownloadedOrHonestlyReported(t *testing.T) {
	k, ch, _ := setup(t)
	if err := ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", MessageID: "om_i", ChatID: "oc_1", ChatType: "p2p", MessageType: "image", Content: `{"image_key":"img_ok"}`}); err != nil {
		t.Fatal(err)
	}
	msg := m(k.PendingMessages(ctx, kernel.Primary, 10))[0]
	if msg.Payload.Str("text") != agents.ImageOnlyText || len(agents.StoredImages(msg.Payload)) != 1 || agents.StoredImages(msg.Payload)[0].MimeType != "image/png" {
		t.Fatalf("image message: %+v", msg.Payload)
	}
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", MessageID: "om_j", ChatID: "oc_1", ChatType: "p2p", MessageType: "image", Content: `{"image_key":"img_gone"}`})
	captured := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "connector.feishu"}))
	if failed, _ := captured[len(captured)-1].Payload["imageFailures"].([]any); len(failed) != 1 {
		t.Fatalf("a message whose image could not be fetched is still captured, with the failure: %+v", captured[len(captured)-1].Payload)
	}
	errs := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "agent.error"}))
	if len(errs) != 1 || !strings.Contains(fmt.Sprint(errs[0].Payload["detail"]), "File not in msg") {
		t.Fatalf("the failure reason is kept: %+v", errs)
	}
	last := m(k.PendingMessages(ctx, kernel.Primary, 10))
	if !strings.Contains(last[len(last)-1].Payload.Str("text"), "下载失败") {
		t.Fatalf("the agent is told the truth: %+v", last[len(last)-1].Payload)
	}
}

func TestOutboxDeliversAnswersWhereTheyBegan(t *testing.T) {
	k, ch, fm := setup(t)
	old := m(k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "agent.reply", ThreadID: "main", Payload: kernel.Payload{"text": "history"}}))
	_ = old
	if n := m(ch.OutboxOnce(ctx)); n != 0 || len(fm.sent) != 0 {
		t.Fatal("a fresh outbox starts at the tail")
	}
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", MessageID: "om_q", ChatID: "oc_9", ChatType: "p2p", MessageType: "text", Content: text("问题")})
	q := m(k.PendingMessages(ctx, kernel.Primary, 10))[0]
	m(k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "agent.reply", ThreadID: "main", Payload: kernel.Payload{"text": "答案", "root": q.ID}}))
	item := m(k.CreateWorkItem(ctx, "写报告", "test", kernel.CreateWorkItemOpts{}))
	long := strings.Repeat("段落内容。\n\n", 800)
	m(k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "agent.reply", ThreadID: item.ThreadID, WorkItemID: item.ID, Payload: kernel.Payload{"text": long, "root": q.ID}}))
	m(k.Append(ctx, kernel.EventInput{Source: "agent:manager", Kind: "agent.reply", ThreadID: item.ThreadID, WorkItemID: item.ID, Payload: kernel.Payload{"text": "补充", "root": q.ID}}))
	m(ch.OutboxOnce(ctx))
	if len(fm.sent) < 5 {
		t.Fatalf("sent: %+v", fm.sent)
	}
	if fm.sent[0].kind != "send" || fm.sent[0].target != "oc_9" || fm.sent[0].text != "答案" || !fm.sent[0].rich {
		t.Fatalf("main-thread answer: %+v", fm.sent[0])
	}
	if fm.sent[1].kind != "send" || fm.sent[1].rich || !strings.HasPrefix(fm.sent[1].text, "📋 "+item.ID) {
		t.Fatalf("the work item thread root is plain text: %+v", fm.sent[1])
	}
	threadRoot := "om_2"
	parts := 0
	for _, s := range fm.sent[2:] {
		if s.kind != "reply" || s.target != threadRoot {
			t.Fatalf("work item answers go into its thread: %+v", s)
		}
		parts++
	}
	if !strings.Contains(fm.sent[2].text, "（1/") {
		t.Fatalf("a long answer is split and numbered, not cut: %q", fm.sent[2].text[len(fm.sent[2].text)-20:])
	}
	if fm.sent[len(fm.sent)-1].text != "补充" {
		t.Fatal("the binding is reused for later answers")
	}
	if b, ok, _ := k.FindByWorkItem(ctx, "feishu", item.ID); !ok || b.RootID != threadRoot {
		t.Fatalf("binding: %+v", b)
	}
	// The person replies in that thread: it goes straight to the work item.
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", MessageID: "om_r", ChatID: "oc_9", MessageType: "text", RootID: threadRoot, Content: text("再改一下")})
	att := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "message.attributed"}))
	if len(att) != 1 || att[0].WorkItemID != item.ID || att[0].Payload.Str("by") != "explicit" {
		t.Fatalf("thread reply attribution: %+v", att)
	}
	_ = parts
}

func TestScheduledAnswersReachTheMainChat(t *testing.T) {
	k, ch, fm := setup(t)
	m(ch.OutboxOnce(ctx))
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", MessageID: "om_1", ChatID: "oc_main", ChatType: "p2p", MessageType: "text", Content: text("hi")})
	prompt, _ := m2(k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{Source: "connector:schedule:sc_1", Kind: "schedule.prompt", Mailbox: kernel.Primary, Payload: kernel.Payload{"name": "日报"}}}))
	m(k.Append(ctx, kernel.EventInput{Source: "agent:primary", Kind: "agent.reply", ThreadID: "main", Payload: kernel.Payload{"text": "今天的总结", "root": prompt.ID}}))
	m(ch.OutboxOnce(ctx))
	last := fm.sent[len(fm.sent)-1]
	if last.target != "oc_main" || last.text != "⏰ 日报\n今天的总结" {
		t.Fatalf("%+v", fm.sent)
	}
}

func m2[A, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}

func TestFormatting(t *testing.T) {
	if got := feishu.FlattenCodeFences("before\n```go\nx := 1\ny := 2\n```\nafter"); got != "before\n    x := 1\n    y := 2\nafter" {
		t.Fatalf("%q", got)
	}
	keys := feishu.ImageKeys("post", `{"content":[[{"tag":"text","text":"a"},{"tag":"img","image_key":"k1"}],[{"tag":"img","image_key":"k2"}]]}`)
	if strings.Join(keys, ",") != "k1,k2" {
		t.Fatal(keys)
	}
	if feishu.SniffMime([]byte{0xff, 0xd8, 0xff, 0}, "image/png") != "image/jpeg" || feishu.SniffMime([]byte("RIFF0000WEBP"), "x") != "image/webp" {
		t.Fatal("sniff")
	}
	chunks := feishu.ChunkText(strings.Repeat("x", 9000), 3500)
	if len(chunks) != 3 || len([]rune(chunks[0])) != 3500 {
		t.Fatalf("an oversized line is hard-wrapped: %d", len(chunks))
	}
	if strings.Join(feishu.ChunkText("a\n\nb", 3500), "|") != "a\n\nb" || feishu.ChunkText("", 10) != nil {
		t.Fatal("short text is one chunk")
	}
	esc := feishu.OutboundText(kernel.Event{Kind: "escalation", Payload: kernel.Payload{"question": "哪台服务器？", "path": []any{map[string]any{"title": "部署", "tried": "查了配置"}}}})
	if esc != "❓ 哪台服务器？\n\n已经尝试过：\n- 部署：查了配置" {
		t.Fatalf("%q", esc)
	}
}

func TestOnlyTheOwnerDrivesTheAgents(t *testing.T) {
	k, ch, _ := setup(t)
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", SenderOpenID: "ou_owner", MessageID: "om_1", ChatID: "oc_owner", ChatType: "p2p", MessageType: "text", Content: text("我是主人")})
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", SenderOpenID: "ou_other", MessageID: "om_2", ChatID: "oc_other", ChatType: "p2p", MessageType: "text", Content: text("帮我跑个命令")})
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", SenderOpenID: "ou_owner", MessageID: "om_3", ChatID: "oc_group", ChatType: "group", MessageType: "text", Content: text("@bot 群里的话")})
	pending := m(k.PendingMessages(ctx, kernel.Primary, 10))
	if len(pending) != 1 || pending[0].Payload.Str("text") != "我是主人" {
		t.Fatalf("with no allowlist only the first private chat is heard: %+v", pending)
	}
	if ignored := m(k.ListEvents(ctx, kernel.ListFilter{Kind: "connector.feishu_ignored"})); len(ignored) != 2 {
		t.Fatalf("other senders are recorded, not obeyed: %+v", ignored)
	}
	ch.Allowed = []string{"ou_other"}
	_ = ch.HandleMessage(ctx, feishu.Inbound{SenderType: "user", SenderOpenID: "ou_other", MessageID: "om_4", ChatID: "oc_group", ChatType: "group", MessageType: "text", Content: text("允许的人")})
	if pending := m(k.PendingMessages(ctx, kernel.Primary, 10)); len(pending) != 2 {
		t.Fatalf("an allowlisted sender is heard anywhere: %d", len(pending))
	}
}
