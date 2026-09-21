package subscription

import "context"

type SubscriptionRepo interface {
	Save(ctx context.Context, channel, recipientID, zone string) error
}
type Service struct{ repo SubscriptionRepo }

func NewService(repo SubscriptionRepo) *Service { return &Service{repo: repo} }
func (s *Service) Subscribe(ctx context.Context, channel, recipientID, zone string) error {
	return s.repo.Save(ctx, channel, recipientID, zone)
}
