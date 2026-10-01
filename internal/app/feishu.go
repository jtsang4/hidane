package app

import (
	"context"
	"log"
	"time"

	"github.com/jtsang4/hidane/internal/feishu"
)

// feishuCredentials come from the environment, else settings.json — an app
// opened from Finder sees no shell environment.
func (a *App) feishuCredentials() (string, string) {
	if a.Cfg.FeishuAppID != "" && a.Cfg.FeishuAppSecret != "" {
		return a.Cfg.FeishuAppID, a.Cfg.FeishuAppSecret
	}
	if f := a.Settings.Get().Feishu; f != nil {
		return f.AppID, f.AppSecret
	}
	return "", ""
}

// startFeishu starts the Feishu channel when it is configured: the long
// connection for inbound messages and the outbox consumer for answers.
func (a *App) startFeishu(ctx context.Context) {
	id, secret := a.feishuCredentials()
	if id == "" || secret == "" {
		return
	}
	ch := feishu.New(a.K, a.Sys, feishu.NewSDK(id, secret))
	a.goBackground(func() { ch.RunOutbox(ctx) })
	a.goBackground(func() {
		for ctx.Err() == nil {
			if err := ch.Listen(ctx, id, secret); err != nil && ctx.Err() == nil {
				log.Printf("feishu long connection: %v (retrying)", err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(10 * time.Second):
			}
		}
	})
	log.Printf("feishu channel enabled (app %s)", id)
}
