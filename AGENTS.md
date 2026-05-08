# file-editor

## Dev commands

```bash
# Backend (Go)
cd backend-go && go run .

# Frontend (Vue 3 + Vite)
cd frontend && npm install && npm run dev       # dev server on :5174
cd frontend && npm run build-go                 # build → ../backend-go/dist

# Docker
docker compose up -d            # config auto-generated in ./config/config.json
```

## Key architecture

- **Backend**: `backend-go/main.go` — pure Go stdlib + `golang.org/x/crypto` + `github.com/pkg/sftp`
- **Frontend**: `frontend/src/App.vue` — Vue 3 + Element Plus + Monaco Editor
- **VFS layer**: `backend-go/vfs/{vfs,local,sftp}.go` — each protocol implements `FileSystem` interface
- Config file: `config/config.json` (auto-generated on first run)
- Go module: `file-editor/backend-go`

## Important gotchas

- **Config path**: `./config/config.json`, NOT `./config.json` (changed during development)
- **SFTP auth**: Connection is verified at add-time in `handleRootsAdd`. Add root dialog sends full config to `POST /api/roots`.
- **Path security**: `resolveFS` (main.go) and `SFTPFS.resolve` (vfs/sftp.go) both check for path traversal independently. Special case: root `/` must not become `//` when appending `/`.
- **WebDAV XML**: `vfs/webdav.go` strips `DAV:` namespace from XML before parsing (`stripDAVNS`). Base path for relative path extraction comes from first PROPFIND response entry's href, NOT from `url.Parse` (Chinese characters in URL path break `url.Parse.Path`).
- **WebDAV 404**: `Stat` handles 404 directly → returns `os.ErrNotExist`. `propfind` helper does NOT handle 404 (used only for directory listing where path is expected to exist).
- **WebDAV deps**: zero external deps, uses Go stdlib `net/http`.
- **PinnedPaths**: file tree favorites stored in `config.json` under `pinnedPaths`. API: `GET/POST/DELETE /api/files/pin`. Injected at root level during directory listing, display filename only (not full path).
- **Root listing**: `handleFilesTree` does NOT Stat remote roots (avoids SSH/network timeout), defaults to directory=true.
- **Copy/move**: Same-root uses FS-native copy/rename. Cross-root uses `crossFSCopy` (read+write).
- **Frontend tree**: Pinned nodes get `treeKey: pin-{rootIndex}-{path}` to avoid key collision with real nodes.
- **User**: single user, no backward compat needed. Commits done manually.
- **Context menu**: "添加为常驻目录" was removed in favor of pin/favorites feature.

## CI/CD

| Workflow | Trigger | Produces |
|----------|---------|----------|
| `.github/workflows/release.yml` | git tag | GitHub release with linux amd64 binary + dist |
| `.github/workflows/docker.yml` | manual dispatch | Docker image → ghcr.io |

## File structure

```
backend-go/           → Go HTTP server + VFS layer
  main.go             → entry, routes, handlers, config
  vfs/                → FileSystem interface + protocol impls
    vfs.go            → FileSystem interface, IdentityInfo
    local.go          → LocalFS (os.* wrappers)
    sftp.go           → SFTPFS (ssh + sftp client)
    webdav.go         → WebDAVFS (net/http client, zero deps)
  file_identity_*.go  → platform-specific UID/GID parsing
frontend/             → Vue 3 + Vite
  src/App.vue         → main UI: file tree, editor, dialogs
  src/MonacoEditor.vue
  src/api.js          → REST client (axios)
```
