// Cloudflare Worker for the Esper launcher backend: signed update manifests,
// release news, and a health endpoint. Static documents live in ../public and
// are served through the ASSETS binding.
const VERSION = "0.1.0";

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    const path = url.pathname;
    const head = request.method === "HEAD";

    if (request.method !== "GET" && !head) {
      return respond(head, "method not allowed\n", 405, {
        allow: "GET, HEAD",
        "content-type": "text/plain; charset=utf-8",
        "cache-control": "no-store",
      });
    }

    if (path === "/healthz") {
      return respond(head, "ok\n", 200, {
        "content-type": "text/plain; charset=utf-8",
        "cache-control": "no-store",
      });
    }

    if (path === "/") {
      const body = JSON.stringify({ service: "esper-api", version: VERSION }) + "\n";
      return respond(head, body, 200, {
        "content-type": "application/json; charset=utf-8",
        "cache-control": "no-store",
      });
    }

    let response;
    try {
      response = await env.ASSETS.fetch(request);
    } catch (error) {
      console.error(`assets fetch failed: ${error}`);
      return respond(head, "internal error\n", 500, {
        "content-type": "text/plain; charset=utf-8",
        "cache-control": "no-store",
      });
    }

    const headers = new Headers(response.headers);
    headers.set("x-content-type-options", "nosniff");
    headers.set("cache-control", response.ok ? cacheControl(path) : "no-store");
    console.log(`${request.method} ${path} ${response.status}`);

    return new Response(head ? null : response.body, {
      status: response.status,
      statusText: response.statusText,
      headers,
    });
  },
};

function respond(head, body, status, headers) {
  return new Response(head ? null : body, { status, headers });
}

// Release documents are short-lived; everything else stays out of caches.
function cacheControl(path) {
  if (path.startsWith("/v1/latest")) return "public, max-age=300";
  if (path.startsWith("/v1/")) return "public, max-age=60";
  return "no-store";
}
