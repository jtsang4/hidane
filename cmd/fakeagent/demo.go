package main

import (
	"encoding/json"
	"os"
	"strings"
)

// A demo script (FAKEAGENT_DEMO=<json file>) gives the fake Primary and
// Managers the words a real model would use, for the README screenshots
// (frontend/e2e/readme-shots.mjs). A message no entry names gets the usual
// scripted answer.
type demoScript struct {
	// Replies are what the Primary answers itself to a message containing When.
	Replies []struct {
		When  string `json:"when"`
		Reply string `json:"reply"`
	} `json:"replies"`
	// Tasks are what the Primary turns into a work item.
	Tasks []demoTask `json:"tasks"`
}

type demoTask struct {
	When          string   `json:"when"`
	Title         string   `json:"title"`
	Repos         []string `json:"repos"`
	Understanding string   `json:"understanding"`
	// Worker is the fake worker's instructions (WRITE <file>: … / RUN: …).
	Worker string `json:"worker"`
	// Ask makes the Manager ask the person instead of starting a worker.
	Ask  string `json:"ask"`
	Done string `json:"done"`
}

func loadDemo() *demoScript {
	path := os.Getenv("FAKEAGENT_DEMO")
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var d demoScript
	if json.Unmarshal(b, &d) != nil {
		return nil
	}
	return &d
}

// demoPrimary answers one person's message from the script.
func demoPrimary(d *demoScript, id, text string) (map[string]any, bool) {
	if d == nil {
		return nil, false
	}
	for _, r := range d.Replies {
		if strings.Contains(text, r.When) {
			return map[string]any{"type": "reply", "of": id, "reply": r.Reply}, true
		}
	}
	for _, t := range d.Tasks {
		if strings.Contains(text, t.When) {
			repos := []any{}
			for _, r := range t.Repos {
				repos = append(repos, map[string]any{"repo": r, "in_place": false})
			}
			return map[string]any{"type": "create_work_item", "of": id, "title": t.Title, "brief": text, "repos": repos, "dispatch": true}, true
		}
	}
	return nil, false
}

// demoManager answers a Manager's turn for a work item the script created.
func demoManager(d *demoScript, prompt, turn string) (string, bool) {
	if d == nil {
		return "", false
	}
	for _, t := range d.Tasks {
		if !strings.Contains(prompt, " — "+t.Title+" (status: ") {
			continue
		}
		switch {
		case strings.Contains(turn, ": ok)"):
			return effects(map[string]any{"type": "reply", "reply": t.Done}), true
		case strings.Contains(turn, "(worker result"):
			return "", false
		case t.Ask != "":
			return effects(map[string]any{"type": "understanding", "text": t.Understanding},
				map[string]any{"type": "escalate", "question": t.Ask, "tried": "read the brief"}), true
		default:
			return effects(map[string]any{"type": "understanding", "text": t.Understanding},
				map[string]any{"type": "spawn", "instructions": t.Worker, "expect": "the change is in place"}), true
		}
	}
	return "", false
}
