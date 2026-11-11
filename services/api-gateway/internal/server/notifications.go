package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"ms/api-gateway/internal/auth"
	notificationv1 "ms/proto/gen/notification/v1"
)

// apiNotificationStream opens an SSE stream for the authenticated user and
// pushes new notifications as they arrive via the in-process hub.
// It also sends the initial unread badge count.
func (a *App) apiNotificationStream(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	sse := datastar.NewSSE(w, r)

	// Push current unread count immediately.
	if resp, err := a.clients.Notification.GetNotifications(r.Context(), &notificationv1.GetNotificationsRequest{
		UserId: user.ID, UnreadOnly: true, Limit: 5,
	}); err == nil {
		_ = sse.PatchElements(renderNotifBadge(resp.GetUnreadCount()))
		for _, n := range resp.GetNotifications() {
			_ = sse.PatchElements(renderNotifToast(n))
		}
	}

	// The notification-service hub is not directly accessible from the gateway
	// (it lives inside the notification-service process). Instead, the gateway
	// polls via gRPC every few seconds and pushes any new unread notifications.
	// A production setup would expose the hub over a gRPC server-streaming RPC.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastCount int32 = -1
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			resp, err := a.clients.Notification.GetNotifications(r.Context(), &notificationv1.GetNotificationsRequest{
				UserId: user.ID, UnreadOnly: true, Limit: 10,
			})
			if err != nil || sse.IsClosed() {
				return
			}
			if resp.GetUnreadCount() != lastCount {
				lastCount = resp.GetUnreadCount()
				_ = sse.PatchElements(renderNotifBadge(lastCount))
				for _, n := range resp.GetNotifications() {
					_ = sse.PatchElements(renderNotifToast(n))
				}
			}
		}
	}
}

// apiMarkNotificationsRead marks all (or specific) notifications as read.
func (a *App) apiMarkNotificationsRead(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	_, err := a.clients.Notification.MarkRead(r.Context(), &notificationv1.MarkReadRequest{
		UserId: user.ID,
	})
	sse := datastar.NewSSE(w, r)
	if err != nil {
		_ = sse.ConsoleError(err)
		return
	}
	_ = sse.PatchElements(`<span id="notif-badge" class="notif-badge hidden">0</span>`)
}

func renderNotifBadge(count int32) string {
	if count == 0 {
		return `<span id="notif-badge" class="notif-badge hidden">0</span>`
	}
	return fmt.Sprintf(`<span id="notif-badge" class="notif-badge">%d</span>`, count)
}

func renderNotifToast(n *notificationv1.Notification) string {
	return fmt.Sprintf(`<div id="toast-%s" class="toast" data-on-animationend="this.remove()">
		<strong>%s</strong><div class="toast-body">%s</div>
	</div>`, n.GetId(), escapeHTML(n.GetTitle()), escapeHTML(n.GetBody()))
}
