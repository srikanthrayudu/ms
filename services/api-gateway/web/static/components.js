/**
 * Swiftride Rocket Web Components
 * Uses datastar-rocket.js to define reusable custom elements that
 * co-exist with Datastar SSE-driven DOM patches from the Go backend.
 *
 * Components:
 *   <sw-fare-meter>      — live animated fare display (booking page)
 *   <sw-eta-counter>     — countdown timer (ride tracking page)
 *   <sw-star-rating>     — interactive 1-5 star rating widget
 *   <sw-surge-pill>      — surge multiplier badge with pulse animation
 *   <sw-ride-type-card>  — individual ride tier selector card
 *   <sw-safety-bar>      — Hopr-style privacy/safety badge row
 *   <sw-trip-stat>       — single stat cell (trips, earnings, avg)
 *   <sw-driver-avatar>   — driver card avatar with ring animation
 */

// datastar-rocket.js re-exports `rocket` as a named export alongside
// all standard Datastar primitives, so we import it here.
import { rocket } from 'https://cdn.jsdelivr.net/gh/starfederation/datastar@v1.0.5/bundles/datastar-rocket.js';

// ─── <sw-fare-meter> ──────────────────────────────────────────────────────
// Attributes:
//   amount     — numeric fare string e.g. "24.50"
//   currency   — "USD" (default)
//   surging    — "" | "true"  (shows surge glow when truthy)
//   label      — description line below the amount
rocket('sw-fare-meter', {
  mode: 'light',
  props: ({ number, string, bool }) => ({
    amount:   number.default(0),
    currency: string.default('USD'),
    surging:  bool.default(false),
    label:    string.default('Estimated fare'),
  }),
  render: ({ html, props: { amount, currency, surging, label } }) => {
    const symbol = currency === 'USD' ? '$' : currency;
    const glow   = surging ? 'style="color:var(--amber);text-shadow:0 0 20px rgba(245,158,11,.5)"' : '';
    return html`
      <div class="sw-fare-meter" style="display:flex;align-items:baseline;gap:4px;">
        <span style="font-size:14px;font-weight:700;color:var(--purple-l);align-self:flex-start;margin-top:6px;">${symbol}</span>
        <span style="font-size:2.4rem;font-weight:900;letter-spacing:-.04em;color:var(--text);transition:color .3s;" ${glow}>
          ${amount.toFixed(2)}
        </span>
        <div style="display:flex;flex-direction:column;margin-left:8px;">
          <span style="font-size:12px;color:var(--muted);">${label}</span>
          ${surging ? html`<span style="font-size:11px;color:var(--amber);font-weight:600;">🔥 Surge active</span>` : ''}
        </div>
      </div>
    `;
  },
});

// ─── <sw-eta-counter> ─────────────────────────────────────────────────────
// Attributes:
//   minutes    — initial countdown value (integer)
//   status     — ride status string from backend
//
// Counts down once per minute. Resets when `minutes` attribute changes.
rocket('sw-eta-counter', {
  mode: 'light',
  props: ({ number, string }) => ({
    minutes: number.default(5),
    status:  string.default('REQUESTED'),
  }),
  setup: ({ props, el }) => {
    let remaining = props.minutes;
    let timer = null;

    const tick = () => {
      remaining = Math.max(0, remaining - 1);
      const display = el.querySelector('.eta-val');
      if (display) display.textContent = remaining;
      if (remaining === 0 && timer) { clearInterval(timer); timer = null; }
    };

    // Start countdown when first mounted
    timer = setInterval(tick, 60_000);

    // Expose reset for external callers (e.g. backend SSE triggers)
    window.resetEta = (mins) => {
      remaining = mins;
      const display = el.querySelector('.eta-val');
      if (display) display.textContent = remaining;
      if (!timer) timer = setInterval(tick, 60_000);
    };

    return () => { if (timer) clearInterval(timer); }; // teardown
  },
  render: ({ html, props: { minutes, status } }) => {
    const labels = {
      REQUESTED:  'Finding driver…',
      MATCHED:    'Arriving in',
      ON_THE_WAY: 'Arriving in',
      ONGOING:    'En route',
      COMPLETED:  'Arrived!',
      CANCELLED:  'Cancelled',
    };
    const label = labels[status] || 'ETA';
    const done  = status === 'COMPLETED' || status === 'CANCELLED';
    return html`
      <div style="text-align:center;">
        <div style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.08em;margin-bottom:4px;">
          ${label}
        </div>
        ${done
          ? html`<div style="font-size:1.8rem;font-weight:900;color:var(--green);">${status === 'COMPLETED' ? '✅' : '❌'}</div>`
          : html`<div style="display:flex;align-items:baseline;justify-content:center;gap:3px;">
              <span class="eta-val" style="font-size:2.5rem;font-weight:900;color:var(--green);line-height:1;">${minutes}</span>
              <span style="font-size:14px;color:var(--muted);">min</span>
            </div>`
        }
      </div>
    `;
  },
});

// ─── <sw-star-rating> ─────────────────────────────────────────────────────
// Attributes:
//   value      — currently selected stars (0-5)
//   readonly   — if set, non-interactive
//
// Emits a custom `sw-rated` event on the element with { detail: { stars } }.
rocket('sw-star-rating', {
  mode: 'light',
  props: ({ number, bool }) => ({
    value:    number.default(0),
    readonly: bool.default(false),
  }),
  setup: ({ el, props }) => {
    let selected = props.value;
    el.addEventListener('click', (e) => {
      if (props.readonly) return;
      const btn = e.target.closest('[data-star]');
      if (!btn) return;
      selected = parseInt(btn.dataset.star, 10);
      el.dispatchEvent(new CustomEvent('sw-rated', { bubbles: true, detail: { stars: selected } }));
      // Highlight stars up to selected
      el.querySelectorAll('[data-star]').forEach((s, i) => {
        s.style.filter   = i < selected ? 'none' : 'grayscale(1) brightness(.5)';
        s.style.transform = i < selected ? 'scale(1.15)' : 'scale(1)';
      });
    });
    el.addEventListener('mouseover', (e) => {
      if (props.readonly) return;
      const btn = e.target.closest('[data-star]');
      if (!btn) return;
      const hover = parseInt(btn.dataset.star, 10);
      el.querySelectorAll('[data-star]').forEach((s, i) => {
        s.style.filter = i < hover ? 'none' : 'grayscale(1) brightness(.5)';
      });
    });
    el.addEventListener('mouseleave', () => {
      el.querySelectorAll('[data-star]').forEach((s, i) => {
        s.style.filter   = i < selected ? 'none' : 'grayscale(1) brightness(.5)';
        s.style.transform = i < selected ? 'scale(1.15)' : 'scale(1)';
      });
    });
  },
  render: ({ html, props: { value, readonly } }) => {
    const stars = [1, 2, 3, 4, 5].map(n => {
      const active = n <= value;
      return html`
        <span
          data-star="${n}"
          style="font-size:32px;cursor:${readonly ? 'default' : 'pointer'};
                 transition:filter .15s,transform .15s;
                 filter:${active ? 'none' : 'grayscale(1) brightness(.5)'};
                 transform:${active ? 'scale(1.15)' : 'scale(1)'};">
          ⭐
        </span>
      `;
    });
    return html`
      <div style="display:flex;gap:8px;justify-content:center;user-select:none;">
        ${stars}
      </div>
    `;
  },
});

// ─── <sw-surge-pill> ──────────────────────────────────────────────────────
// Attributes:
//   multiplier — float, e.g. "1.8"
//   visible    — "" | "true"
rocket('sw-surge-pill', {
  mode: 'light',
  props: ({ number, bool }) => ({
    multiplier: number.default(1.0),
    visible:    bool.default(false),
  }),
  render: ({ html, props: { multiplier, visible } }) => {
    if (!visible || multiplier <= 1.0) return html`<span></span>`;
    const level = multiplier >= 2.0 ? 'var(--red)' : multiplier >= 1.5 ? 'var(--amber)' : 'var(--amber)';
    return html`
      <span style="
        display:inline-flex;align-items:center;gap:5px;
        background:rgba(245,158,11,.12);
        border:1px solid rgba(245,158,11,.3);
        color:${level};
        font-size:12px;font-weight:700;
        padding:4px 12px;border-radius:100px;
        animation:pulse-badge 2s ease infinite;">
        🔥 ${multiplier.toFixed(1)}× Surge
      </span>
    `;
  },
});

// ─── <sw-ride-type-card> ──────────────────────────────────────────────────
// Attributes:
//   icon      — emoji
//   name      — display name
//   desc      — subtitle
//   price     — formatted price string
//   selected  — "" | "true"
// Emits `sw-select` on click.
rocket('sw-ride-type-card', {
  mode: 'light',
  props: ({ string, bool }) => ({
    icon:     string.default('🚗'),
    name:     string.default('SwiftGo'),
    desc:     string.default('Economy'),
    price:    string.default('—'),
    selected: bool.default(false),
  }),
  setup: ({ el }) => {
    el.style.cursor = 'pointer';
    el.addEventListener('click', () => {
      el.dispatchEvent(new CustomEvent('sw-select', { bubbles: true, detail: { name: el.getAttribute('name') } }));
    });
  },
  render: ({ html, props: { icon, name, desc, price, selected } }) => {
    const border = selected ? '1.5px solid var(--purple)' : '1.5px solid var(--border)';
    const bg     = selected ? 'rgba(139,92,246,.1)' : 'var(--panel)';
    const glow   = selected ? 'box-shadow:0 0 24px rgba(139,92,246,.35);' : '';
    const check  = selected ? html`<span style="position:absolute;top:6px;right:8px;font-size:12px;color:var(--purple-l);">✓</span>` : '';
    return html`
      <div style="
        position:relative;
        background:${bg};border:${border};border-radius:14px;
        padding:14px 10px;text-align:center;
        transition:all .15s;${glow}">
        ${check}
        <div style="font-size:28px;margin-bottom:6px;">${icon}</div>
        <div style="font-size:13px;font-weight:700;color:var(--text);margin-bottom:2px;">${name}</div>
        <div style="font-size:11px;color:var(--muted);margin-bottom:6px;">${desc}</div>
        <div style="font-size:15px;font-weight:800;color:var(--purple-l);">${price}</div>
      </div>
    `;
  },
});

// ─── <sw-safety-bar> ──────────────────────────────────────────────────────
// Renders a row of safety + privacy badges.
// Attribute `badges` — JSON array of {label, icon, privacy?} objects.
rocket('sw-safety-bar', {
  mode: 'light',
  props: ({ string }) => ({
    badges: string.default('[]'),
  }),
  render: ({ html, props: { badges } }) => {
    let items = [];
    try { items = JSON.parse(badges); } catch {}
    if (!items.length) {
      items = [
        { icon: '✅', label: 'Verified driver' },
        { icon: '📍', label: 'Trip shared' },
        { icon: '🛡️', label: 'Private routing', privacy: true },
        { icon: '🔒', label: 'Encrypted events', privacy: true },
      ];
    }
    const chips = items.map(b => {
      const color = b.privacy ? 'rgba(139,92,246,.08)' : 'rgba(16,185,129,.08)';
      const border = b.privacy ? 'rgba(139,92,246,.2)' : 'rgba(16,185,129,.2)';
      const text   = b.privacy ? 'var(--purple-xl)' : '#6ee7b7';
      return html`
        <span style="
          display:inline-flex;align-items:center;gap:5px;
          background:${color};border:1px solid ${border};
          color:${text};font-size:12px;font-weight:500;
          padding:5px 11px;border-radius:100px;">
          ${b.icon} ${b.label}
        </span>
      `;
    });
    return html`<div style="display:flex;gap:8px;flex-wrap:wrap;">${chips}</div>`;
  },
});

// ─── <sw-trip-stat> ───────────────────────────────────────────────────────
// Attributes: label, value, sub, accent ("green"|"purple"|"amber")
rocket('sw-trip-stat', {
  mode: 'light',
  props: ({ string }) => ({
    label:  string.default('Stat'),
    value:  string.default('—'),
    sub:    string.default(''),
    accent: string.default('purple'),
  }),
  render: ({ html, props: { label, value, sub, accent } }) => {
    const colors = { green: 'var(--green)', purple: 'var(--purple-l)', amber: 'var(--amber)' };
    const color  = colors[accent] || 'var(--text)';
    return html`
      <div style="
        background:var(--panel);border:1px solid var(--border);
        border-radius:14px;padding:18px 16px;text-align:center;">
        <span style="font-size:11px;color:var(--muted);text-transform:uppercase;letter-spacing:.07em;display:block;margin-bottom:6px;">${label}</span>
        <span style="font-size:1.5rem;font-weight:900;color:${color};display:block;">${value}</span>
        ${sub ? html`<span style="font-size:11px;color:var(--muted);display:block;margin-top:2px;">${sub}</span>` : ''}
      </div>
    `;
  },
});

// ─── <sw-driver-avatar> ───────────────────────────────────────────────────
// Attributes: initials, online ("true"|"")
rocket('sw-driver-avatar', {
  mode: 'light',
  props: ({ string, bool }) => ({
    initials: string.default('D'),
    online:   bool.default(false),
  }),
  render: ({ html, props: { initials, online } }) => {
    const ring = online ? html`
      <div style="
        position:absolute;inset:-4px;border-radius:50%;
        border:2px solid var(--green);
        animation:pulse-ring 1.5s ease infinite;pointer-events:none;">
      </div>` : '';
    return html`
      <div style="position:relative;width:52px;height:52px;flex-shrink:0;">
        <div style="
          width:52px;height:52px;border-radius:50%;
          background:linear-gradient(135deg,var(--purple),var(--pink));
          display:flex;align-items:center;justify-content:center;
          font-size:22px;font-weight:800;color:#fff;
          box-shadow:0 0 24px rgba(139,92,246,.4);">
          ${initials}
        </div>
        ${ring}
        ${online ? html`<div style="
          position:absolute;bottom:1px;right:1px;
          width:13px;height:13px;border-radius:50%;
          background:var(--green);border:2px solid var(--bg);"></div>` : ''}
      </div>
    `;
  },
});

// ─── Wire Rocket components into existing pages ────────────────────────────
// The booking page and ride page use vanilla JS for map interaction,
// but we enhance them with Rocket components after DOMContentLoaded.

document.addEventListener('DOMContentLoaded', () => {

  // Booking page: replace static ride type grid cards with <sw-ride-type-card>
  const grid = document.getElementById('ride-type-grid');
  if (grid) {
    // Listen for sw-select events from rocket cards to sync the pool signal
    grid.addEventListener('sw-select', (e) => {
      // Find selected card and call existing selectRideType logic
      const cards = grid.querySelectorAll('sw-ride-type-card, .ride-type-card');
      cards.forEach(c => c.removeAttribute('selected'));
      e.target.setAttribute('selected', 'true');
    });
  }

  // Replace static safety rows on booking + ride pages with <sw-safety-bar>
  document.querySelectorAll('.safety-row[data-rocket]').forEach(el => {
    const bar = document.createElement('sw-safety-bar');
    const badges = el.dataset.badges;
    if (badges) bar.setAttribute('badges', badges);
    el.replaceWith(bar);
  });

  // Ride page: wire backend SSE status updates to <sw-eta-counter>
  // The backend calls window.onRideStatusUpdate(st, driverName, vehicle, fare, surge)
  const originalStatusUpdate = window.onRideStatusUpdate;
  window.onRideStatusUpdate = function(st, driverName, vehicle, fare, surge) {
    // Update rocket ETA counter
    const eta = document.querySelector('sw-eta-counter');
    if (eta) eta.setAttribute('status', st);

    // Update rocket surge pill
    const pill = document.querySelector('sw-surge-pill');
    if (pill) {
      pill.setAttribute('multiplier', String(surge || 1.0));
      pill.setAttribute('visible', surge && surge > 1 ? 'true' : '');
    }

    // Update rocket fare meter when completed
    if (st === 'COMPLETED' && fare) {
      const meter = document.querySelector('sw-fare-meter');
      if (meter) {
        meter.setAttribute('amount', String(fare));
        meter.setAttribute('label', 'Final fare');
        meter.setAttribute('surging', surge && surge > 1 ? 'true' : '');
      }
    }

    // Update rocket driver avatar online state
    const avatar = document.querySelector('sw-driver-avatar');
    if (avatar) {
      const isActive = ['MATCHED','ON_THE_WAY','ONGOING'].includes(st);
      avatar.setAttribute('online', isActive ? 'true' : '');
    }

    if (originalStatusUpdate) originalStatusUpdate(st, driverName, vehicle, fare, surge);
  };

  // Ride page: wire star rating component
  document.addEventListener('sw-rated', (e) => {
    console.log('Swiftride: rated', e.detail.stars, 'stars');
    // Could POST to /api/rides/{id}/rate — backend endpoint not yet wired,
    // but the event fires correctly for future integration.
    const submitBtn = document.getElementById('rating-submit-btn');
    if (submitBtn) submitBtn.disabled = false;
  });

  // Driver page: sync earnings stat via Rocket component if present
  const originalRideStatus = window.onRideStatus;
  window.onRideStatus = function(s, fare) {
    if (s === 'COMPLETED' && fare) {
      const earningStat = document.querySelector('sw-trip-stat[data-id="earnings"]');
      if (earningStat) {
        const cur = parseFloat(earningStat.getAttribute('value').replace('$','')) || 0;
        earningStat.setAttribute('value', '$' + (cur + parseFloat(fare)).toFixed(2));
      }
      const tripStat = document.querySelector('sw-trip-stat[data-id="trips"]');
      if (tripStat) {
        const cur = parseInt(tripStat.getAttribute('value'), 10) || 0;
        tripStat.setAttribute('value', String(cur + 1));
      }
    }
    if (originalRideStatus) originalRideStatus(s, fare);
  };
});
