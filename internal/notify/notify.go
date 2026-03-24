package notify

import "context"

type Notifier interface {
	SendMessageWithContext(ctx context.Context, chatID, message string) error
	GetPlatform() string
}

type Registry struct {
	notifiers map[string]Notifier
}

func NewRegistry() *Registry {
	return &Registry{
		notifiers: make(map[string]Notifier),
	}
}

func (r *Registry) Register(notifier Notifier) {
	r.notifiers[notifier.GetPlatform()] = notifier
}

func (r *Registry) Get(platform string) Notifier {
	return r.notifiers[platform]
}

func (r *Registry) Notify(ctx context.Context, platform, chatID, message string) error {
	notifier := r.Get(platform)
	if notifier == nil {
		return nil
	}
	return notifier.SendMessageWithContext(ctx, chatID, message)
}

type contextKey string

const (
	PlatformKey       contextKey = "platform"
	PlatformChatIDKey contextKey = "platform_chat_id"
)

func WithPlatform(ctx context.Context, platform, chatID string) context.Context {
	ctx = context.WithValue(ctx, PlatformKey, platform)
	ctx = context.WithValue(ctx, PlatformChatIDKey, chatID)
	return ctx
}

func GetPlatformFromContext(ctx context.Context) (platform string, chatID string) {
	if v := ctx.Value(PlatformKey); v != nil {
		platform = v.(string)
	}
	if v := ctx.Value(PlatformChatIDKey); v != nil {
		chatID = v.(string)
	}
	return
}
