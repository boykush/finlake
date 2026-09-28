# check=skip=InvalidDefaultArgInFrom

# Go の版は workflow が .mise.toml から読んで渡す。決め先を .mise.toml だけに保つため
# ARG に既定値を置かず、それを咎める check を先頭で外している。
ARG GO_VERSION

# DuckDB は cgo で静的リンクされるので、runtime と glibc を揃える（bookworm = debian12）。
FROM golang:${GO_VERSION}-bookworm AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd cmd
COPY internal internal
RUN CGO_ENABLED=1 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/finlake ./cmd/finlake
# httpfs（R2 などの S3 互換ストレージを読み書きする拡張）はビルド時に入れ、実行時にネットワークから取りに行かない。
# 同じバイナリで入れるので、拡張と DuckDB 本体の版が必ず揃う。
RUN /out/finlake duckdb-extensions /out/duckdb-extensions

# DuckDB が libstdc++ を要るので static ではなく cc。
FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=build /out/finlake /usr/local/bin/finlake
COPY --from=build /out/duckdb-extensions /usr/local/share/duckdb/extensions
ENV FINLAKE_DUCKDB_EXTENSION_DIRECTORY=/usr/local/share/duckdb/extensions
EXPOSE 8080
# Job も MCP サーバーも同じイメージで、サブコマンドで使い分ける。
ENTRYPOINT ["/usr/local/bin/finlake"]
CMD ["mcp"]
