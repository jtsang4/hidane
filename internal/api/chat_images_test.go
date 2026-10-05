package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jtsang4/hidane/internal/api"
)

// An image that cannot go to a model is dropped, not the message it came with.
func TestChatDropsImagesItCannotSend(t *testing.T) {
	e := newEnv(t, api.Options{})
	png := map[string]any{"mimeType": "image/png", "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="}
	for want, images := range map[float64][]any{
		1: {map[string]any{"mimeType": "text/plain", "data": "aGk="}, png},
		2: {map[string]any{"mimeType": "image/png", "data": strings.Repeat("A", 8<<20+4)}, png, png},
		4: {png, png, png, png, png},
	} {
		code, body := e.do("POST", "/api/chat", "", map[string]any{"text": "look", "images": images})
		if code != 202 {
			t.Fatalf("%d %v", code, body)
		}
		msg, _, _ := e.k.GetEvent(context.Background(), body["messageId"].(string))
		if msg.Payload["imageCount"] != want {
			t.Fatalf("kept %v images, want %v", msg.Payload["imageCount"], want)
		}
	}
}
