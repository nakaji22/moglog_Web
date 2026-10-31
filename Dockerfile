# もぐログ Web版 - コンテナイメージ
# ビルド: docker build -t moglog-web .
# 実行:   docker run -p 8080:8080 -v moglog-data:/data moglog-web
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/moglog-web .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
  && adduser -D -H appuser \
  && mkdir -p /data && chown appuser:appuser /data
COPY --from=build /out/moglog-web /usr/local/bin/moglog-web
USER appuser
ENV PORT=8080 DATA_DIR=/data
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["moglog-web"]
