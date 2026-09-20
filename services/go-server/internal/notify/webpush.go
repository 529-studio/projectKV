package notify

import (
	"context"
	"errors"
)

type WebPushChannel struct{}

func NewWebPushChannel() *WebPushChannel { return &WebPushChannel{} }
func (c *WebPushChannel) Name() string   { return "webpush" }
func (c *WebPushChannel) Send(ctx context.Context, recipientID string, alert Alert) error {
	return errors.New("not implemented")
}
