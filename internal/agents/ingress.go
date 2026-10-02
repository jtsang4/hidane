package agents

import (
	"context"
	"fmt"

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
	// ReplyTo is an event the person replied to; its work item becomes the target.
	ReplyTo string
	// Focus marks a target that came from what was focused rather than an explicit pick.
	Focus bool
	// Channel coordinates the outbox needs to answer on the same surface.
	Channel map[string]any
	// RunAs is what the person chose to run the work on: it pins the target
	// work item, or the one the Primary creates for this message. Nil keeps
	// what is configured.
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

// SubmitMessage is the one door every person's message comes through, whatever
// the channel. Recording and delivery are the same append: the message lands on
// the main thread and is addressed to whoever should read it. Nothing waits.
//
// Attribution is cheapest-first: an explicit target or a reply to something
// that belongs to a work item needs no model; only the rest go to the Primary.
func (s *System) SubmitMessage(ctx context.Context, m InboundMessage) (kernel.Event, error) {
	k := s.K
	target := m.Target
	by := ""
	if target != "" {
		by = "explicit"
		if m.Focus {
			by = "focus"
		}
	}
	answers := ""
	if m.ReplyTo != "" {
		ref, ok, err := k.GetEvent(ctx, m.ReplyTo)
		if err != nil {
			return kernel.Event{}, err
		}
		if ok && ref.Kind == "escalation" {
			answers = ref.ID
		}
		if target == "" && ok && ref.WorkItemID != "" {
			target, by = ref.WorkItemID, "explicit"
		}
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
	if m.RunAs != nil && m.RunAs.Agent != "" {
		payload["runAs"] = map[string]any{"agent": m.RunAs.Agent, "provider": m.RunAs.Provider, "model": m.RunAs.Model, "effort": m.RunAs.Effort}
	}
	if target != "" {
		item, err := k.GetWorkItem(ctx, target)
		if err != nil {
			return kernel.Event{}, err
		}
		if m.RunAs != nil && m.RunAs.Agent != "" {
			if item, err = k.SetWorkItemRunAs(ctx, item.ID, m.RunAs, m.Source); err != nil {
				return kernel.Event{}, err
			}
		}
		payload["target"] = item.ID
		ev, err := k.Append(ctx, kernel.EventInput{Source: m.Source, Kind: "user.message", ThreadID: "main", WorkItemID: item.ID, Payload: payload})
		if err != nil {
			return ev, err
		}
		_, err = s.DeliverToWorkItem(ctx, ev, item, by, DeliverOpts{Answers: answers})
		return ev, err
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
}

// DeliverToWorkItem hands a main-thread message to a work item's Manager and
// records who decided. The Manager's copy lives on the item's thread; the
// main-thread original stays the person's record. A closed item the person
// talks to again is reopened.
func (s *System) DeliverToWorkItem(ctx context.Context, message kernel.Event, item kernel.WorkItem, by string, o DeliverOpts) (kernel.Event, error) {
	k := s.K
	source := o.Source
	if source == "" {
		source = message.Source
		if by == "model" {
			source = "agent:primary"
		}
	}
	if item.Status != kernel.StatusOpen {
		if _, err := k.SetWorkItemStatus(ctx, item.ID, kernel.StatusOpen, source); err != nil {
			return kernel.Event{}, err
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
		return kernel.Event{}, err
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
	ev, _, err := k.Post(ctx, kernel.PostInput{EventInput: kernel.EventInput{
		Source: source, Kind: "user.message", Mailbox: kernel.ManagerAddress(item.ID), Lane: kernel.LaneInterrupt,
		ThreadID: item.ThreadID, WorkItemID: item.ID, Payload: fwd,
	}, CausedBy: &message})
	return ev, err
}

// Reattribute: the person moved a message to another work item (or answered
// "which one?"). The earlier decision stays in the log; the newest wins.
func (s *System) Reattribute(ctx context.Context, messageID, workItemID, source string) error {
	k := s.K
	message, ok, err := k.GetEvent(ctx, messageID)
	if err != nil {
		return err
	}
	if !ok || message.Kind != "user.message" {
		return fmt.Errorf("message not found: %w", kernel.ErrNotFound)
	}
	item, err := k.GetWorkItem(ctx, workItemID)
	if err != nil {
		return err
	}
	prior, err := k.ListEvents(ctx, kernel.ListFilter{Kind: "message.attributed", PayloadKey: "of", PayloadValue: messageID})
	if err != nil {
		return err
	}
	previous := ""
	if len(prior) > 0 {
		previous = prior[len(prior)-1].WorkItemID
	}
	if previous == item.ID {
		return nil
	}
	_, err = s.DeliverToWorkItem(ctx, message, item, "user", DeliverOpts{Source: source, Previous: previous})
	return err
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
			result = clipRunes(text, 6000)
			if result != text {
				// A silent cut reads as the child's whole answer.
				result += fmt.Sprintf("\n[truncated — the full answer is in work item %s; its files are in %s]", sib.ID, sib.Workspace)
			}
		}
		results = append(results, map[string]any{"workItemId": sib.ID, "title": sib.Title, "status": sib.Status, "result": result})
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
