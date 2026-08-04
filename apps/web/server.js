import http from "node:http";
import { handler } from "./build/handler.js";

const API_URL = new URL(process.env.ORACLE_API_URL ?? "http://localhost:8080");
const PORT = Number(process.env.PORT ?? 3000);

// Paths owned by the Go API. Everything else is SvelteKit.
function shouldProxy(pathname) {
  return pathname.startsWith("/api/") || pathname.startsWith("/auth/") || pathname === "/health";
}

function proxy(req, res) {
  const upstream = http.request(
    {
      hostname: API_URL.hostname,
      port: API_URL.port,
      method: req.method,
      path: req.url,
      headers: { ...req.headers, host: API_URL.host },
    },
    (upstreamRes) => {
      res.writeHead(upstreamRes.statusCode ?? 502, upstreamRes.headers);
      upstreamRes.pipe(res);
    },
  );

  upstream.on("error", () => {
    if (!res.headersSent) {
      res.writeHead(502, { "Content-Type": "application/json" });
    }
    res.end(JSON.stringify({ error: "bad gateway" }));
  });

  req.pipe(upstream);
}

http
  .createServer((req, res) => {
    const pathname = new URL(req.url ?? "/", "http://localhost").pathname;
    if (shouldProxy(pathname)) {
      proxy(req, res);
      return;
    }
    handler(req, res);
  })
  .listen(PORT, () => {
    console.log(`oracle web listening on :${PORT}, proxying /api and /auth to ${API_URL.href}`);
  });
