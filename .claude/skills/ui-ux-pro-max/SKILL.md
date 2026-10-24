---
name: ui-ux-pro-max
description: UI/UX design intelligence for web, mobile and desktop — layout, typography, color systems, accessibility, interaction states, RTL/Persian support, dashboards and Telegram bot UX. Use when designing or reviewing an interface, building a front-end component, or improving usability of an existing screen or bot flow.
---

# UI/UX Pro Max

## Start from the user, not the widget

1. Who is using it, on what device, in what situation? What is the one primary action on this screen?
2. Map the flow: entry → primary action → success/failure → next step. Remove every step that does not serve the goal.
3. Then design the screen.

## Visual system

- **Spacing**: 4/8 px scale; consistent gutters; group related items, separate unrelated ones (proximity beats borders).
- **Typography**: max 2 families; type scale (e.g. 12/14/16/20/24/32); body 16px on web; line length 45–80 chars; line-height 1.4–1.6. For Persian use Vazirmatn / IRANSans-like fonts and Persian digits where appropriate.
- **Color**: define tokens (`--bg`, `--surface`, `--text`, `--muted`, `--primary`, `--danger`, `--success`, `--warning`). One accent color for primary actions. Support dark mode by redefining tokens, not by inverting.
- **Hierarchy**: size, weight and contrast before color. One primary button per view.

## Interaction

- Every interactive element has hover, focus-visible, active, disabled and loading states.
- Show feedback within 100 ms; skeletons for loads > 300 ms; optimistic UI where safe.
- Empty states explain what to do next; error states say what happened and how to fix it, in plain language.
- Destructive actions need confirmation or undo.
- Forms: labels above fields, inline validation on blur, keep user input on error.

## Accessibility (non-negotiable)

- WCAG AA contrast (4.5:1 text, 3:1 large text/UI).
- Full keyboard navigation, logical focus order, visible focus ring.
- Semantic HTML (`button`, `nav`, `main`, `label for`), `alt` text, `aria-*` only when semantics are insufficient.
- Touch targets ≥ 44×44 px; respect `prefers-reduced-motion`.

## RTL / Persian

- `dir="rtl"` on the root; use logical CSS properties (`margin-inline-start`, `padding-inline-end`, `inset-inline`).
- Mirror directional icons (arrows, chevrons), not universal ones (play, check).
- Numbers, dates (Jalali where expected) and currency formatted with `Intl` (`fa-IR`).

## Telegram bot UX

- Short messages; one decision per message; inline keyboards with ≤ 3 buttons per row and clear verbs.
- Edit the existing message for step changes instead of spamming new ones; always give a "back"/"cancel" path.
- Confirm payments and irreversible actions explicitly; show status with emoji sparingly and consistently.

## Responsive

Mobile-first; breakpoints by content, not devices; no horizontal scroll at 360 px; tables collapse to cards on small screens.

## Review output

List issues ordered by user impact (blocker → polish), each with the location, the problem, and a concrete fix.
