# Frontend build (Vue SPA)

The browser UI is a Vue 3 + Vite app under `web/`.

Build for production (output: `web/dist/`):

```bash
npm run build:web
```

Develop with hot reload (proxies `/api` to the Go server on `:8080`):

```bash
npm run dev:web
```

CI: `.github/workflows/build-assets.yml` builds the SPA on pushes/PRs to `main`.  
Go tests also build the SPA in `.github/workflows/go-test.yml`.

See [`web/README.md`](web/README.md) for route list and auth notes.
