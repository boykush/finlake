# check=skip=InvalidDefaultArgInFrom

# Node と pnpm の版は workflow が .mise.toml から読んで渡す（決め先を .mise.toml だけに保つ）。
ARG NODE_VERSION
ARG NODE_MAJOR

FROM node:${NODE_VERSION}-bookworm-slim AS build
ARG PNPM_VERSION
WORKDIR /app
RUN npm install --global "pnpm@${PNPM_VERSION}"
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM gcr.io/distroless/nodejs${NODE_MAJOR}-debian12:nonroot
WORKDIR /app
ENV NODE_ENV=production PORT=3000 HOSTNAME=0.0.0.0 NEXT_TELEMETRY_DISABLED=1
# standalone の出力は静的ファイルを含まないので、server.js の隣に置く。
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
EXPOSE 3000
CMD ["server.js"]
