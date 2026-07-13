FROM node:22-bookworm-slim@sha256:f3a68cf41a855d227d1b0ab832bed9749469ef38cf4f58182fb8c893bc462383

RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /linters
COPY .github/linters/package.json /linters/package.json
RUN npm install --legacy-peer-deps

ENV PATH="/linters/node_modules/.bin:${PATH}"
ENV NODE_PATH="/linters/node_modules"

COPY .github/linters/entrypoint.sh /usr/local/bin/lint-entrypoint.sh
RUN chmod +x /usr/local/bin/lint-entrypoint.sh

WORKDIR /app

ENTRYPOINT ["/usr/local/bin/lint-entrypoint.sh"]
CMD ["all"]
