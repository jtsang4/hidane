package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jtsang4/hidane/internal/clip"
	"github.com/jtsang4/hidane/internal/kernel"
	"github.com/jtsang4/hidane/internal/settings"
)

// InboundMessage is a person's message from any channel.
type InboundMessage struct {
	Text   string
	Source string
	Images []InboundImage
	// Target is a work item the person addressed directly (focused card, Feishu thread).
	Target string
	// Targets are more work items addressed in the same message (an @ in the
	// composer). Every one of them gets it, each through its own Manager.
	Targets []string
	// ReplyTo is an event the person replied to; its work item becomes the target.
	ReplyTo string
	// Focus marks a target that came from what was focused rather than an explicit pick.
	Focus bool
	// Channel coordinates the outbox needs to answer on the same surface.
	Channel map[string]any
	// RunAs is what the person chose to run the work on: it pins the target
	// work item, or the one the Primary creates for this message. Nil keeps
	// what is configured; otherwise it names an agent (callers validate it).
	RunAs *kernel.RunAs
}

// runAsOf reads the choice a person's message carried.
func runAsOf(m kernel.Event) *kernel.RunAs {
	raw, ok := m.Payload["runAs"].(map[string]any)
	if !ok {
		return nil
	}
	r := &kernel.RunAs{Agent: Str(raw["agent"]), Provider: Str(raw["provider"]), Model: Str(raw["model"]), Effort: Str(raw["effort"])}
	if r.Agent == "" {
		return nil
	}
	return r
}

// ownRun is a work item's own choice as a role configuration (nil: none).
func ownRun(item kernel.WorkItem) *settings.RoleConfig {
	if item.RunAs == nil {
		return nil
	}
	r := item.RunAs
	return &settings.RoleConfig{Agent: r.Agent, Provider: r.Provider, Model: r.Model, Effort: r.Effort}
}

// ErrUndeliverable means the hop budget refused the message.
var ErrUndeliverable = fmt.Errorf("message could not be delivered")

// MaxTargets bounds how many work items one message may address.
const MaxTargets = 8

var (
	ErrTooManyTargets = fmt.Errorf("a message can address at most %d work items", MaxTargets)
	// ErrRunAsNeedsOneTarget: what a task runs on is chosen for one task at a time.
	ErrRunAsNeedsOneTarget = errors.New("choosing what a task runs on needs exactly one addressed work item")
)

// addressed is every work item the message names, the explicit Target first.
func addressed(m InboundMessage) []string {
	var out []string
	seen := map[string]bool{}
	for _, id := range append([]string{m.Target}, m.Targets...) {
		if id = strings.TrimSpace(id); id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// SubmitMessage is the one door every person's message comes through, whatever
// the channel. Recording and delivery are the same append: the message lands on
// the main thread and is addressed to whoever should read it. Nothing waits.
//
// Attribution is cheapest-first: an explicit target or a reply to something
// that belongs to a work item needs no model; only the rest go to the Primary.
func (s *System) SubmitMessage(ctx context.Context, m InboundMessage) (kernel.Event, error) {
	k := s.K
	targets := addressed(m)
	if len(targets) > MaxTargets {
		return kernel.Event{}, ErrTooManyTargets
	}
	if len(targets) > 1 && m.RunAs != nil {
		return kernel.Event{}, ErrRunAsNeedsOneTarget
	}
	answers, answersItem := "", ""
	if m.ReplyTo != "" {
		ref, ok, err := k.GetEvent(ctx, m.ReplyTo)
		if err != nil {
			return kernel.Event{}, err
		}
		if ok && ref.Kind == "escalation" {
			answers, answersItem = ref.ID, ref.WorkItemID
		}
		if len(targets) == 0 && ok && ref.WorkItemID != "" {
			targets = []string{ref.WorkItemID}
		}
	}
	// The first target came from what was focused, not from a pick.
	byOf := func(i int) string {
		if i == 0 && m.Target != "" && m.Focus {
			return "focus"
		}
		return "explicit"
	}
	images, err := s.StoreImages(m.Images)
	if err != nil {
		return kernel.Event{}, err
	}
	payload := kernel.Payload{"text": m.Text}
	if len(images) > 0 {
		payload["images"] = images
		payload["imageCount"] = len(images)
	}
	if m.ReplyTo != "" {
		payload["replyTo"] = m.ReplyTo
	}
	if m.Channel != nil {
		payload["channel"] = m.Channel
	}
	if m.RunAs != nil {
		payload["runAs"] = map[string]any{"agent": m.RunAs.Agent, "provider": m.RunAs.Provider, "model": m.RunAs.Model, "effort": m.RunAs.Effort}
	}
	if len(targets) == 1 {
		item, err := k.GetWorkItem(ctx, targets[0])
		if err != nil {
			return kernel.Event{}, err
		}
		if m.RunAs != nil {
			if item, err = k.SetWorkItemRunAs(ctx, item.ID, m.RunAs, m.Source); err != nil {
				return kernel.Event{}, err
			}
		}
		payload["target"] = item.ID
		ev, err := k.Append(ctx, kernel.EventInput{Source: m.Source, Kind: "user.message", ThreadID: "main", WorkItemID: item.ID, Payload: payload})
		if err != nil {
			return ev, err
		}
		return ev, s.DeliverToWorkItem(ctx, ev, item, byOf(0), DeliverOpts{Answers: answers})
	}
	if len(targets) > 1 {
		var items []kernel.WorkItem
		for _, id := range targets {
			item, err := k.GetWorkItem(ctx, id)
			if err != nil {
				return kernel.Event{}, err
			}
			items = append(items, item)
		}
		list := make([]any, len(items))
		for i, item := range items {
			list[i] = item.ID
		}
		payload["targets"] = list
		// One message on the main thread; it belongs to no single work item.
		ev, err := k.Append(ctx, kernel.EventInput{Source: m.Source, Kind: "user.message", ThreadID: "main", Payload: payload})
		if err != nil {
			return ev, err
		}
		for i, item := range items {
			var others []kernel.WorkItem
			for _, o := range items {
				if o.ID != item.ID {
					others = append(others, o)
				}
			}
			o := DeliverOpts{AlsoTo: others}
			if item.ID == answersItem {
				o.Answers = answers
			}
			if err := s.DeliverToWorkItem(ctx, ev, item, byOf(i), o); err != nil {
				return ev, err
			}
		}
		return ev, nil
	}
	ev, ok, err := k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{
		Source: m.Source, Kind: "user.message", Mailbox: kernel.Primary, Lane: kernel.LaneInterrupt, ThreadID: "main", Payload: payload,
	}})
	if err != nil {
		return ev, err
	}
	if !ok {
		return ev, ErrUndeliverable
	}
	return ev, nil
}

type DeliverOpts struct {
	Text       string
	Confidence *float64
	Answers    string
	Previous   string
	Created    bool
	Source     string
	// AlsoTo are the other work items the same message went to.
	AlsoTo []kernel.WorkItem
	// Context is what the Manager should know besides the person's words; it
	// is not shown as something they said.
	Context string
}

// DeliverToWorkItem hands a main-thread message to a work item's Manager and
// records who decided. The Manager's copy lives on the item's thread; the
// main-thread original stays the person's record. A closed item the person
// talks to again is reopened.
func (s *System) DeliverToWorkItem(ctx context.Context, message kernel.Event, item kernel.WorkItem, by string, o DeliverOpts) error {
	k := s.K
	source := o.Source
	if source == "" {
		source = message.Source
	}
	if item.Status != kernel.StatusOpen {
		if _, err := k.SetWorkItemStatus(ctx, item.ID, kernel.StatusOpen, source); err != nil {
			return err
		}
	}
	attributed := kernel.Payload{"of": message.ID, "workItemId": item.ID, "title": item.Title, "by": by}
	if o.Confidence != nil {
		attributed["confidence"] = *o.Confidence
	}
	if o.Previous != "" {
		attributed["previous"] = o.Previous
	}
	if o.Created {
		attributed["created"] = true
	}
	if _, err := k.Append(ctx, kernel.EventInput{Source: source, Kind: "message.attributed", ThreadID: "main",
		WorkItemID: item.ID, CausedBy: message.ID, Payload: attributed}); err != nil {
		return err
	}
	text := o.Text
	if text == "" {
		text = message.Payload.Str("text")
	}
	fwd := kernel.Payload{"text": text, "of": message.ID, "root": kernel.RootOf(message), "forwardedFrom": "main"}
	if imgs, ok := message.Payload["images"]; ok {
		fwd["images"] = imgs
	}
	if o.Answers != "" {
		fwd["answers"] = o.Answers
	}
	if len(o.AlsoTo) > 0 {
		others := make([]any, len(o.AlsoTo))
		for i, other := range o.AlsoTo {
			others[i] = map[string]any{"workItemId": other.ID, "title": other.Title}
		}
		fwd["alsoTo"] = others
	}
	if o.Context != "" {
		fwd["context"] = o.Context
	}
	_, _, err := k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{
		Source: source, Kind: "user.message", Mailbox: kernel.ManagerAddress(item.ID), Lane: kernel.LaneInterrupt,
		ThreadID: item.ThreadID, WorkItemID: item.ID, Payload: fwd,
	}, CausedBy: &message})
	return err
}

// Reattribute: the person moved a message to another work item, or to a new
// one (workItemID "new"), or answered "which one?". The earlier decision stays
// in the log; the newest wins. It returns where the message now belongs.
func (s *System) Reattribute(ctx context.Context, messageID, workItemID, source string) (kernel.WorkItem, error) {
	k := s.K
	message, ok, err := k.GetEvent(ctx, messageID)
	if err != nil {
		return kernel.WorkItem{}, err
	}
	if !ok || message.Kind != "user.message" {
		return kernel.WorkItem{}, fmt.Errorf("message not found: %w", kernel.ErrNotFound)
	}
	prior, err := k.ListEvents(ctx, kernel.ListFilter{Kind: "message.attributed", PayloadKey: "of", PayloadValue: messageID})
	if err != nil {
		return kernel.WorkItem{}, err
	}
	previous := ""
	if len(prior) > 0 {
		previous = prior[len(prior)-1].WorkItemID
	}
	if workItemID == "new" {
		return s.startFromMessage(ctx, message, source, DeliverOpts{Previous: previous})
	}
	item, err := k.GetWorkItem(ctx, workItemID)
	if err != nil || previous == item.ID {
		return item, err
	}
	return item, s.DeliverToWorkItem(ctx, message, item, "user", DeliverOpts{Source: source, Previous: previous})
}

// titleFromMessage names a work item after what the person said.
func titleFromMessage(text string) string {
	if title := clip.Runes(strings.Join(strings.Fields(text), " "), 60); title != "" {
		return title
	}
	return "新任务"
}

// ErrAlreadyAttributed: the message already went to a work item.
var ErrAlreadyAttributed = errors.New("the message already belongs to a work item")

// Promote makes a work item of something the Primary answered in the
// conversation: the person decided it needs following, not only an answer.
// The Manager gets the person's words, and what was already answered as
// context rather than as something they said.
func (s *System) Promote(ctx context.Context, messageID, source string) (kernel.WorkItem, error) {
	k := s.K
	message, ok, err := k.GetEvent(ctx, messageID)
	if err != nil {
		return kernel.WorkItem{}, err
	}
	if !ok || message.Kind != "user.message" || message.ThreadID != "main" || message.Payload.Bool("redacted") {
		return kernel.WorkItem{}, fmt.Errorf("message not found: %w", kernel.ErrNotFound)
	}
	prior, err := k.ListEvents(ctx, kernel.ListFilter{Kind: "message.attributed", PayloadKey: "of", PayloadValue: messageID})
	if err != nil {
		return kernel.WorkItem{}, err
	}
	if len(prior) > 0 {
		return kernel.WorkItem{}, ErrAlreadyAttributed
	}
	answers, err := k.ListEvents(ctx, kernel.ListFilter{Kind: "agent.reply", PayloadKey: "root", PayloadValue: messageID})
	if err != nil {
		return kernel.WorkItem{}, err
	}
	var said []string
	for _, a := range answers {
		if a.WorkItemID == "" && strings.TrimSpace(a.Payload.Str("text")) != "" {
			said = append(said, a.Payload.Str("text"))
		}
	}
	answered := ""
	if len(said) > 0 {
		answered = "The Primary already answered this in the conversation:\n" + clip.Noted(strings.Join(said, "\n\n"), 4000)
	}
	return s.startFromMessage(ctx, message, source, DeliverOpts{Context: answered})
}

// startFromMessage is how the person makes a work item of their message: it
// runs on what the message chose, and the message is attributed to it as created.
func (s *System) startFromMessage(ctx context.Context, message kernel.Event, source string, o DeliverOpts) (kernel.WorkItem, error) {
	item, problem, err := s.StartWorkItem(ctx, titleFromMessage(message.Payload.Str("text")), source, kernel.CreateWorkItemOpts{Of: message.ID}, nil)
	if err != nil {
		return item, err
	}
	if problem != "" {
		return item, errors.New(problem)
	}
	if r := runAsOf(message); r != nil {
		if item, err = s.K.SetWorkItemRunAs(ctx, item.ID, r, source); err != nil {
			return item, err
		}
	}
	o.Source, o.Created = source, true
	return item, s.DeliverToWorkItem(ctx, message, item, "user", o)
}

// RedactMessage hides something the person said. Only their own main-thread
// messages qualify; hiding twice records nothing new. What a Manager already
// read is not recalled — hiding is about what is shown and fed forward.
func (s *System) RedactMessage(ctx context.Context, messageID, source string) (*kernel.Event, error) {
	k := s.K
	message, ok, err := k.GetEvent(ctx, messageID)
	if err != nil {
		return nil, err
	}
	if !ok || message.Kind != "user.message" || message.ThreadID != "main" {
		return nil, fmt.Errorf("message not found: %w", kernel.ErrNotFound)
	}
	if message.Payload.Bool("redacted") {
		return nil, nil
	}
	ev, err := k.Append(ctx, kernel.EventInput{Source: source, Kind: "message.redacted", ThreadID: "main",
		WorkItemID: message.WorkItemID, CausedBy: message.ID, Payload: kernel.Payload{"of": message.ID, "root": kernel.RootOf(message)}})
	return &ev, err
}

// ChangeStatus is the one path for status changes, so a parent always hears
// when its children have all settled.
func (s *System) ChangeStatus(ctx context.Context, id, status, source string, causedBy *kernel.Event) (kernel.WorkItem, error) {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	item, err := s.K.SetWorkItemStatus(ctx, id, status, source)
	if err != nil {
		return item, err
	}
	if item.Parent() != "" && status != kernel.StatusOpen {
		if err := s.notifyParentIfSettled(ctx, item, causedBy); err != nil {
			return item, err
		}
	}
	return item, nil
}

func (s *System) notifyParentIfSettled(ctx context.Context, child kernel.WorkItem, causedBy *kernel.Event) error {
	k := s.K
	siblings, err := k.ListChildren(ctx, child.Parent())
	if err != nil {
		return err
	}
	for _, sib := range siblings {
		if sib.Status == kernel.StatusOpen {
			return nil
		}
	}
	parent, err := k.GetWorkItem(ctx, child.Parent())
	if err != nil {
		return err
	}
	var results []any
	for _, sib := range siblings {
		last, err := k.ListEvents(ctx, kernel.ListFilter{WorkItemID: sib.ID, Kind: "agent.reply", Tail: 1})
		if err != nil {
			return err
		}
		result := ""
		if len(last) > 0 {
			text := last[0].Payload.Str("text")
			result = clip.Runes(text, 6000)
			if result != text {
				// A silent cut reads as the child's whole answer.
				result += fmt.Sprintf("\n[truncated — the full answer is in work item %s; its files are in %s]", sib.ID, sib.Workspace)
			}
		}
		entry := map[string]any{"workItemId": sib.ID, "title": sib.Title, "status": sib.Status, "result": result}
		// Where the child's work is, for the parent to merge it back.
		if held, err := s.checkoutsOf(ctx, sib.ID, ""); err == nil {
			var branches []any
			for _, c := range held.list {
				if c.Mode == kernel.CheckoutWorktree && c.Branch != "" {
					branches = append(branches, held.repos[c.RepoID].Name+": "+c.Branch)
				}
			}
			if len(branches) > 0 {
				entry["branches"] = branches
			}
		}
		results = append(results, entry)
	}
	payload := kernel.Payload{"children": results}
	if causedBy != nil {
		payload["root"] = kernel.RootOf(*causedBy)
	}
	_, _, err = k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{
		Source: "kernel:runtime", Kind: "children.settled", Mailbox: kernel.ManagerAddress(parent.ID), Lane: kernel.LaneNormal,
		ThreadID: parent.ThreadID, WorkItemID: parent.ID, Payload: payload,
	}, CausedBy: causedBy})
	return err
}

// ParentMailbox is where a bubbling fact goes next.
func ParentMailbox(item kernel.WorkItem) string {
	if p := item.Parent(); p != "" {
		return kernel.ManagerAddress(p)
	}
	return kernel.Primary
}
