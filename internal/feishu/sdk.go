package feishu

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
)

func encodeBase64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// SDK is the Messenger over the official Feishu SDK, which owns token refresh
// and the wire protocol.
type SDK struct {
	client *lark.Client
}

func NewSDK(appID, appSecret string) *SDK {
	return &SDK{client: lark.NewClient(appID, appSecret)}
}

func apiError(code int, msg string) error { return fmt.Errorf("feishu %d: %s", code, msg) }

func (s *SDK) SendText(ctx context.Context, chatID, text string, rich bool) (string, error) {
	msgType, content := "interactive", MarkdownCard(text)
	if !rich {
		msgType = "text"
		content = larkim.NewTextMsgBuilder().Text(text).Build()
	}
	resp, err := s.client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().ReceiveId(chatID).MsgType(msgType).Content(content).Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", apiError(resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

func (s *SDK) ReplyInThread(ctx context.Context, rootMessageID, text string) (string, error) {
	resp, err := s.client.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().
		MessageId(rootMessageID).
		Body(larkim.NewReplyMessageReqBodyBuilder().MsgType("interactive").Content(MarkdownCard(text)).ReplyInThread(true).Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", apiError(resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

// FetchImage surfaces Feishu's own reason on failure: a guessed cause once
// sent debugging down the wrong path when the message had been recalled.
func (s *SDK) FetchImage(ctx context.Context, messageID, key string) ([]byte, string, error) {
	resp, err := s.client.Im.MessageResource.Get(ctx, larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).FileKey(key).Type("image").Build())
	if err != nil {
		return nil, "", err
	}
	if !resp.Success() {
		return nil, "", apiError(resp.Code, resp.Msg)
	}
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, resp.File); err != nil {
		return nil, "", err
	}
	header := ""
	if resp.ApiResp != nil {
		header = resp.ApiResp.Header.Get("Content-Type")
	}
	return buf.Bytes(), header, nil
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Listen holds the long connection until ctx ends. The handler acknowledges at
// once and runs the chain detached: the platform expects a fast ack.
func (c *Channel) Listen(ctx context.Context, appID, appSecret string) error {
	handler := dispatcher.NewEventDispatcher("", "").OnP2MessageReceiveV1(func(_ context.Context, ev *larkim.P2MessageReceiveV1) error {
		if ev == nil || ev.Event == nil || ev.Event.Message == nil {
			return nil
		}
		in := Inbound{
			MessageID:   deref(ev.Event.Message.MessageId),
			ChatID:      deref(ev.Event.Message.ChatId),
			ChatType:    deref(ev.Event.Message.ChatType),
			MessageType: deref(ev.Event.Message.MessageType),
			RootID:      deref(ev.Event.Message.RootId),
			Content:     deref(ev.Event.Message.Content),
		}
		if ev.EventV2Base != nil && ev.EventV2Base.Header != nil {
			in.EventID = ev.EventV2Base.Header.EventID
		}
		if ev.Event.Sender != nil {
			in.SenderType = deref(ev.Event.Sender.SenderType)
			if ev.Event.Sender.SenderId != nil {
				in.SenderOpenID = deref(ev.Event.Sender.SenderId.OpenId)
			}
		}
		go func() {
			if err := c.HandleMessage(ctx, in); err != nil && ctx.Err() == nil {
				log.Printf("feishu message: %v", err)
				_, _ = c.K.Append(ctx, kernelError(err))
			}
		}()
		return nil
	})
	cli := larkws.NewClient(appID, appSecret, larkws.WithEventHandler(handler), larkws.WithLogLevel(larkcore.LogLevelWarn))
	return cli.Start(ctx)
}
