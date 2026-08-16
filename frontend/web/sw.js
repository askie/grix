const CACHE_PREFIX = "grix-";
const OFFLINE_FALLBACK = "index.html";
const BUILD_QUERY_PARAM = "build";
const DEFAULT_BUILD_ID = "dev";
const DEFAULT_NOTIFICATION_TITLE = "Grix";
const DEFAULT_NOTIFICATION_BODY = "你有新消息";
const DEFAULT_NOTIFICATION_ICON = "icons/Icon-192.png";

function getRawBuildId() {
  const currentUrl = new URL(self.location.href);
  const rawBuildId = currentUrl.searchParams.get(BUILD_QUERY_PARAM) || "";
  return rawBuildId.trim() || DEFAULT_BUILD_ID;
}

function buildCacheKey(buildId) {
  return buildId.replace(/[^a-zA-Z0-9._-]/g, "-");
}

const BUILD_ID = getRawBuildId();
const BUILD_CACHE_KEY = buildCacheKey(BUILD_ID);
const APP_SHELL_CACHE = `${CACHE_PREFIX}app-shell-${BUILD_CACHE_KEY}`;
const RUNTIME_CACHE = `${CACHE_PREFIX}runtime-${BUILD_CACHE_KEY}`;

function getScopePath() {
  return new URL(self.registration.scope).pathname;
}

function toScopeRelativeUrl(path) {
  return new URL(path, self.registration.scope).toString();
}

function withBuildQuery(path) {
  const url = new URL(path, self.registration.scope);
  if (BUILD_ID !== DEFAULT_BUILD_ID) {
    url.searchParams.set(BUILD_QUERY_PARAM, BUILD_ID);
  }
  return url.toString();
}

function isHttpRequest(request) {
  return request.url.startsWith("http://") || request.url.startsWith("https://");
}

function isNavigationRequest(request) {
  return request.mode === "navigate";
}

function isApiRequest(url) {
  return url.pathname.startsWith("/v1/") || url.pathname.endsWith("/ws");
}

function isControlPlaneRequest(url) {
  const scopePath = getScopePath();
  if (!url.pathname.startsWith(scopePath)) {
    return false;
  }

  const relativePath = url.pathname.slice(scopePath.length);
  return relativePath === "manifest.json" || relativePath === "version.json";
}

function isAppAsset(url) {
  const scopePath = getScopePath();
  if (!url.pathname.startsWith(scopePath)) {
    return false;
  }

  if (isApiRequest(url) || isControlPlaneRequest(url)) {
    return false;
  }

  const relativePath = url.pathname.slice(scopePath.length);
  if (!relativePath) {
    return true;
  }

  return (
    [
      "main.dart.js",
      "main.dart.mjs",
      "main.dart.wasm",
      "favicon.png",
      "sqlite3.wasm",
      "sqflite_sw.js",
    ].includes(relativePath) ||
    relativePath.startsWith("icons/") ||
    relativePath.startsWith("splash/") ||
    relativePath.startsWith("font-fallbacks/") ||
    relativePath.startsWith("canvaskit/")
  );
}

function normalizeBadgeCount(value) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed) || parsed <= 0) {
    return 0;
  }
  return Math.floor(parsed);
}

async function syncAppBadge(count) {
  const normalized = normalizeBadgeCount(count);
  try {
    if (
      self.registration &&
      typeof self.registration.setAppBadge === "function" &&
      typeof self.registration.clearAppBadge === "function"
    ) {
      if (normalized > 0) {
        await self.registration.setAppBadge(normalized);
      } else {
        await self.registration.clearAppBadge();
      }
      return;
    }
  } catch (error) {
    console.warn("Service worker registration badge update failed:", error);
  }

  try {
    if (
      self.navigator &&
      typeof self.navigator.setAppBadge === "function" &&
      typeof self.navigator.clearAppBadge === "function"
    ) {
      if (normalized > 0) {
        await self.navigator.setAppBadge(normalized);
      } else {
        await self.navigator.clearAppBadge();
      }
    }
  } catch (error) {
    console.warn("Worker navigator badge update failed:", error);
  }
}

function normalizeNotificationPayload(rawData) {
  if (!rawData || typeof rawData !== "object") {
    return {
      title: DEFAULT_NOTIFICATION_TITLE,
      body: DEFAULT_NOTIFICATION_BODY,
      sessionId: "",
      badge: 0,
      badgeOnly: false,
    };
  }

  const title =
    typeof rawData.title === "string" && rawData.title.trim()
      ? rawData.title.trim()
      : DEFAULT_NOTIFICATION_TITLE;
  const body =
    typeof rawData.body === "string" && rawData.body.trim()
      ? rawData.body.trim()
      : DEFAULT_NOTIFICATION_BODY;
  const sessionId =
    typeof rawData.session_id === "string" ? rawData.session_id.trim() : "";
  const badge = normalizeBadgeCount(rawData.badge);
  const badgeOnly = rawData.badge_only === true;

  return { title, body, sessionId, badge, badgeOnly };
}

function buildNotificationTargetUrl(sessionId) {
  const base = new URL("./", self.registration.scope);
  if (!sessionId) {
    return base.toString();
  }
  base.searchParams.set("session_id", sessionId);
  return base.toString();
}

async function focusOrOpenClient(targetUrl) {
  const clientsList = await self.clients.matchAll({
    type: "window",
    includeUncontrolled: true,
  });
  for (const client of clientsList) {
    if ("focus" in client) {
      if ("navigate" in client) {
        try {
          await client.navigate(targetUrl);
        } catch (_) {
          // Ignore navigate errors and still focus existing window.
        }
      }
      return client.focus();
    }
  }
  if (self.clients.openWindow) {
    return self.clients.openWindow(targetUrl);
  }
  return undefined;
}

async function precacheAppShell() {
  const cache = await caches.open(APP_SHELL_CACHE);
  const urls = [
    toScopeRelativeUrl("./"),
    toScopeRelativeUrl("index.html"),
    withBuildQuery("main.dart.js"),
    withBuildQuery("main.dart.mjs"),
    withBuildQuery("main.dart.wasm"),
    toScopeRelativeUrl("favicon.png"),
    toScopeRelativeUrl("icons/Icon-192.png"),
    toScopeRelativeUrl("icons/Icon-512.png"),
    toScopeRelativeUrl("icons/Icon-maskable-192.png"),
    toScopeRelativeUrl("icons/Icon-maskable-512.png"),
    toScopeRelativeUrl("splash/img/light-1x.png"),
    toScopeRelativeUrl("splash/img/dark-1x.png"),
  ];
  await Promise.all(
    urls.map(async (url) => {
      try {
        const response = await fetch(url);
        if (response.ok) {
          await cache.put(url, response);
        }
      } catch (_) {
        // skip missing resources (e.g. wasm/mjs in dart2js builds)
      }
    })
  );
}

async function cleanupOutdatedCaches() {
  const keys = await caches.keys();
  await Promise.all(
    keys
      .filter(
        (key) =>
          key.startsWith(CACHE_PREFIX) &&
          key !== APP_SHELL_CACHE &&
          key !== RUNTIME_CACHE
      )
      .map((key) => caches.delete(key))
  );
}

self.addEventListener("install", (event) => {
  event.waitUntil(
    (async () => {
      await precacheAppShell();
      await self.skipWaiting();
    })()
  );
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      await cleanupOutdatedCaches();
      await self.clients.claim();
    })()
  );
});

self.addEventListener("message", (event) => {
  if (event.data?.type === "SKIP_WAITING") {
    event.waitUntil(self.skipWaiting());
  }
});

self.addEventListener("push", (event) => {
  event.waitUntil(
    (async () => {
      let rawData = null;
      try {
        rawData = event.data ? event.data.json() : null;
      } catch (_) {
        rawData = null;
      }

      const payload = normalizeNotificationPayload(rawData);
      const targetUrl = buildNotificationTargetUrl(payload.sessionId);
      const notificationTag = payload.badgeOnly
        ? "badge-sync"
        : payload.sessionId
        ? `session-${payload.sessionId}`
        : "general";
      await Promise.all([
        self.registration.showNotification(payload.title, {
          body: payload.body,
          icon: DEFAULT_NOTIFICATION_ICON,
          badge: DEFAULT_NOTIFICATION_ICON,
          tag: notificationTag,
          renotify: false,
          data: {
            targetUrl,
            sessionId: payload.sessionId,
            badgeOnly: payload.badgeOnly,
          },
        }),
        syncAppBadge(payload.badge),
      ]);
    })()
  );
});

self.addEventListener("notificationclick", (event) => {
  event.notification.close();
  const data = event.notification.data || {};
  const targetUrl =
    typeof data.targetUrl === "string" && data.targetUrl.trim()
      ? data.targetUrl
      : buildNotificationTargetUrl("");
  event.waitUntil(focusOrOpenClient(targetUrl));
});

self.addEventListener("fetch", (event) => {
  const { request } = event;
  if (!isHttpRequest(request) || request.method !== "GET") {
    return;
  }

  const url = new URL(request.url);
  if (isApiRequest(url) || isControlPlaneRequest(url)) {
    return;
  }

  if (isNavigationRequest(request)) {
    event.respondWith(
      (async () => {
        const cache = await caches.open(APP_SHELL_CACHE);
        try {
          const response = await fetch(request);
          // 只缓存成功响应：发布窗口内拿到的错误页/非 200 一旦写入缓存，
          // cache-first 会让客户端永远读到坏副本，卡在启动 loading。
          if (response.ok) {
            cache.put(toScopeRelativeUrl(OFFLINE_FALLBACK), response.clone());
          }
          return response;
        } catch (error) {
          const fallbackResponse = await cache.match(
            toScopeRelativeUrl(OFFLINE_FALLBACK)
          );
          if (fallbackResponse) {
            return fallbackResponse;
          }
          throw error;
        }
      })()
    );
    return;
  }

  if (!isAppAsset(url)) {
    return;
  }

  event.respondWith(
    (async () => {
      const cache = await caches.open(RUNTIME_CACHE);
      const cachedResponse = await cache.match(request);
      if (cachedResponse) {
        return cachedResponse;
      }

      const response = await fetch(request);
      // 同上：非 200（部署窗口 404、WAF 错误页等）不写入运行时缓存，
      // 下次请求回源重试，避免坏副本月级驻留（缓存按 build 隔离，不自愈）。
      if (response.ok) {
        cache.put(request, response.clone());
      }
      return response;
    })()
  );
});
