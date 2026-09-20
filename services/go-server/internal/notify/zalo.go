package notify

import (
	"context"
	"errors"
)

type ZaloChannel struct{}

func NewZaloChannel() *ZaloChannel  { return &ZaloChannel{} }
func (c *ZaloChannel) Name() string { return "zalo" }
func (c *ZaloChannel) Send(ctx context.Context, recipientID string, alert Alert) error {
	return errors.New("not implemented")
}
