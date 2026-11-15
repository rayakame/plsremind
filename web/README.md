# plsremind dashboard

The web interface of plsremind. It is a single-page app that the hub serves from its own
binary. It talks to the hub only through the [TypeScript SDK](../sdk/ts/README.md): one
gateway connection keeps the user's lists, items and plans in memory, every view reads
from there, and changes show at once and are confirmed by the hub afterwards.

## Develop

```sh
npm install        # at the repository root
cd web
npm run dev
```

The dev server forwards `/api`, including the gateway, to a hub on
`http://localhost:8080`; set `PLSREMIND_DEV_HUB` to use another one. Start that hub with
`PLSREMIND_PUBLIC_URL=http://localhost:5173`, the dev server's address: the hub only
accepts a login from the address it believes it is reached at, and it hands out the
gateway's address from the same setting.

```sh
npm run check      # types and formatting
npm test           # unit and component tests against a fake hub
npm run test:e2e   # a real browser against a real hub; needs Docker and Go
npm run build && npm run size   # the start page's scripts must stay under 200 kB gzipped
```

`npm run test:e2e` builds the dashboard into the hub (`make web` in `hub/`), starts
PostgreSQL in Docker and the hub next to it, and needs Chromium once:
`npx playwright install chromium`.

## How it is put together

- `src/style/system.css` is the only place with colours, sizes and spacing. Everything
  else uses its classes and tokens; a test fails on a raw value anywhere else.
- `src/ui/` holds the building blocks, `src/shell/` the frame around every page,
  `src/pages/` the pages.
- `src/hub/` connects to the hub: the session, hooks that read the SDK's optimistic view,
  and the writes that show a change before the hub confirmed it.
- `src/model/` derives what the views show (groups, counts, search) from the cache.
- `src/bar/` is the bar on every page with its commands, `src/keys/` the single-key
  shortcuts.
- `src/time/` does all date arithmetic in the profile's time zone.

## Add a language

Copy `src/i18n/en.ts` to a file named after the language tag, translate the values, and
add it to `languages` in `src/i18n/index.ts` and to `languageNames` in
`src/pages/settings/personal.tsx`. The type checker and a test make sure no key is
missing. The words for typed dates (`when.today`, `when.weekdays`, ...) are in the same
file; English is always understood as well.
