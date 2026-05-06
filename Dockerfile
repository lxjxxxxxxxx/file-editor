# ---- Stage 1: Build frontend ----
FROM node:20-alpine AS frontend
WORKDIR /app
COPY frontend/package*.json ./frontend/
RUN cd frontend && npm ci
COPY frontend/ ./frontend/
RUN cd frontend && npm run build-go

# ---- Stage 2: Build Go backend ----
FROM golang:1.22-alpine AS backend
WORKDIR /app
COPY backend-go/ .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o file-editor-backend .

# ---- Stage 3: Runtime ----
FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app
COPY --from=frontend /app/backend-go/dist ./dist
COPY --from=backend /app/file-editor-backend .
COPY <<"EOF" /entrypoint.sh
#!/bin/sh
set -e
# 如果挂载进来的 config.json 被 Docker 创建成了目录，删掉并写入默认配置
if [ -d /app/config.json ]; then
  rm -rf /app/config.json
fi
if [ ! -f /app/config.json ]; then
  cat > /app/config.json << 'CONFIG_EOF'
{
  "token": "file-editor-2024-secret-token",
  "port": 3002,
  "rootPaths": [],
  "excludedNames": [],
  "excludeHidden": false,
  "textExtensions": [],
  "textFileNames": [],
  "binaryExtensions": [],
  "binaryFileNames": []
}
CONFIG_EOF
fi
exec ./file-editor-backend
EOF
RUN chmod +x /entrypoint.sh
EXPOSE 3002
ENTRYPOINT ["/entrypoint.sh"]
