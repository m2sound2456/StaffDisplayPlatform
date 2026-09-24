# Frontend — Staff Display Platform

React 19 + Vite 7 + TypeScript + Tailwind 4 + PWA (`vite-plugin-pwa`).
One SPA serves the Admin application, the store display and the device setup —
routed by path, never by subdomain (BLUEPRINT §32).

| Route             | Screen                                       |
| ----------------- | -------------------------------------------- |
| `/`               | platform landing + API status                |
| `/app`            | Admin application shell (auth in FG4)        |
| `/s/{store-slug}` | store display for one tenant (tablet, kiosk) |
| `/setup`          | device setup / QR pairing                    |

## Commands

```bash
npm install          # install dependencies
npm run dev          # dev server on http://127.0.0.1:5173 (/api proxied to :8080)
npm run typecheck    # tsc --noEmit (strict)
npm run lint         # eslint (flat config, typescript-eslint)
npm run format       # prettier --write
npm run test         # vitest run (jsdom)
npm run build        # typecheck + production build -> dist/
npm run preview      # serve the production build locally
npm run icons        # regenerate public/icons + favicon.svg (scripts/generate-icons.ps1)
```

## Environment

| Variable                  | Default                 | Purpose                                     |
| ------------------------- | ----------------------- | ------------------------------------------- |
| `VITE_API_BASE_URL`       | `/api/v1`               | REST base (same origin in production)       |
| `VITE_DEFAULT_STORE_SLUG` | `demo`                  | slug used by landing shortcuts              |
| `VITE_DEV_API_TARGET`     | `http://127.0.0.1:8080` | dev proxy target (read by `vite.config.ts`) |

## Structure

```text
src/
├── app/          router + providers (App, routes)
├── components/   layout (PageShell, DisplayShell) and ui primitives
├── features/     feature slices (health; display/employees/devices later)
├── lib/          pure helpers (store slug rules, formatting)
├── pages/        route components (landing, admin, display, setup, 404)
├── services/     API access only — no fetch calls inside components
├── db/           offline cache (FG28)
├── pwa/          service worker registration
├── styles/       Tailwind v4 entry + design tokens
└── test/         test setup and shared mocks
```

## Conventions

- All HTTP goes through `src/services/apiClient.ts` (envelope aware, `ApiError`).
- Slugs are validated with `src/lib/storeSlug.ts` on both client and server.
- Display screens must stay kiosk friendly: no admin navigation, big type, safe areas.
- Tests live next to the code as `*.test.ts(x)` and never hit the network (`src/test/mocks.ts`).
