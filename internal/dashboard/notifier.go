package dashboard

import (
	"context"

	"go-claw/internal/notify"
)

type DashboardNotifier struct {
	wsServer WebSocketNotifier
}

func NewDashboardNotifier(wsServer WebSocketNotifier) *DashboardNotifier {
	return &DashboardNotifier{wsServer: wsServer}
}

func (n *DashboardNotifier) SendMessageWithContext(ctx context.Context, chatID, message string) error {
	if n.wsServer == nil {
		return nil
	}
	payload := map[string]interface{}{
		"type":    "notification",
		"message": message,
	}
	n.wsServer.BroadcastNewMessage(chatID, payload)
	return nil
}

func (n *DashboardNotifier) GetPlatform() string {
	return "dashboard"
}

var _ notify.Notifier = (*DashboardNotifier)(nil)
