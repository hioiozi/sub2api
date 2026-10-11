FROM weishaw/sub2api:0.2.15@sha256:da0fd94daebf890419e21e956821fd6c2008fc666f9617f2eb917f4492245da3
# Keep the official runtime/entrypoint; replace only the binary with embedded UI.
COPY --chown=sub2api:sub2api sub2api /app/sub2api
RUN chmod 755 /app/sub2api
LABEL io.gateway.skin="sakura" io.gateway.base-version="0.2.15"
