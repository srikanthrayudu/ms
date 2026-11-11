package server

import (
	"fmt"
	"net/http"

	"github.com/starfederation/datastar-go/datastar"

	"ms/api-gateway/internal/auth"
	paymentv1 "ms/proto/gen/payment/v1"
)

// apiListPayments returns the authenticated user's payment history as a Datastar patch.
func (a *App) apiListPayments(w http.ResponseWriter, r *http.Request) {
	user := auth.FromContext(r.Context())
	role := r.URL.Query().Get("role")
	if role == "" {
		role = user.Role
	}
	resp, err := a.clients.Payment.ListPayments(r.Context(), &paymentv1.ListPaymentsRequest{
		UserId: user.ID, Role: role, Limit: 20,
	})
	sse := datastar.NewSSE(w, r)
	if err != nil {
		_ = sse.PatchElements(fmt.Sprintf(`<div id="payment-list" class="error">%s</div>`, escapeHTML(err.Error())))
		return
	}
	_ = sse.PatchElements(renderPaymentList(resp))
}

// apiGetPayment returns a single ride's payment.
func (a *App) apiGetPayment(w http.ResponseWriter, r *http.Request) {
	resp, err := a.clients.Payment.GetPayment(r.Context(), &paymentv1.GetPaymentRequest{
		RideId: r.PathValue("rideId"),
	})
	sse := datastar.NewSSE(w, r)
	if err != nil {
		_ = sse.PatchElements(`<div id="payment-detail" class="error">Payment not found.</div>`)
		return
	}
	_ = sse.PatchElements(renderPaymentCard(resp))
}

func renderPaymentList(resp *paymentv1.ListPaymentsResponse) string {
	if len(resp.GetPayments()) == 0 {
		return `<div id="payment-list" class="muted">No payments yet.</div>`
	}
	rows := ""
	for _, p := range resp.GetPayments() {
		statusLabel := p.GetStatus().String()
		rows += fmt.Sprintf(`<tr>
			<td><code>%s</code></td>
			<td><code>%s</code></td>
			<td class="money">$%.2f</td>
			<td><span class="badge %s">%s</span></td>
		</tr>`, p.GetId()[:12], p.GetRideId()[:12], p.GetAmount(),
			paymentStatusClass(p.GetStatus()), statusLabel)
	}
	return fmt.Sprintf(`<div id="payment-list">
		<div class="total-row">Total earned/spent: <strong>$%.2f</strong></div>
		<table class="payment-table">
			<thead><tr><th>Payment</th><th>Ride</th><th>Amount</th><th>Status</th></tr></thead>
			<tbody>%s</tbody>
		</table>
	</div>`, resp.GetTotal(), rows)
}

func renderPaymentCard(p *paymentv1.Payment) string {
	return fmt.Sprintf(`<div id="payment-detail" class="card">
		<h3>Payment <code>%s</code></h3>
		<div>Ride: <code>%s</code></div>
		<div>Amount: <strong>$%.2f</strong></div>
		<div>Status: <span class="badge %s">%s</span></div>
	</div>`, p.GetId(), p.GetRideId(), p.GetAmount(),
		paymentStatusClass(p.GetStatus()), p.GetStatus().String())
}

func paymentStatusClass(s paymentv1.PaymentStatus) string {
	switch s {
	case paymentv1.PaymentStatus_COMPLETED:
		return "done"
	case paymentv1.PaymentStatus_FAILED:
		return "cancelled"
	case paymentv1.PaymentStatus_REFUNDED:
		return "coming"
	default:
		return "pending"
	}
}

// pagePayments renders the payment history page.
func (a *App) pagePayments(w http.ResponseWriter, r *http.Request) {
	u := auth.FromContext(r.Context())
	a.render(w, "payments", PageData{User: u, UserName: a.userName(r.Context(), u)})
}
