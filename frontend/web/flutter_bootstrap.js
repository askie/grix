{{flutter_js}}
{{flutter_build_config}}

const FONT_FALLBACK_MANIFEST_URL = "font-fallback-manifest.json";
const FONT_PREFETCH_CONCURRENCY = 8;
const FONT_PREFETCH_TIMEOUT_MS = 4000;
const FIRST_FRAME_TIMEOUT_MS = 4000;
const POST_FIRST_FRAME_SETTLE_FRAMES = 2;
const APP_BUILD_INFO_URL = "version.json";
const APP_SERVICE_WORKER_URL = "sw.js";
const APP_UPDATE_CHECK_INTERVAL_MS = 60 * 1000;
const APP_UPDATE_FETCH_TIMEOUT_MS = 4000;

let currentAppBuildId = null;
let isPageReloadScheduled = false;
const observedServiceWorkerRegistrations = new WeakSet();

function hideBootstrapOverlay() {
  const overlay = document.getElementById("app-bootstrap-overlay");
  if (!overlay) {
    return;
  }

  overlay.classList.add("app-bootstrap-overlay-hidden");
  window.setTimeout(() => {
    overlay.remove();
  }, 220);
}

function nextAnimationFrame() {
  return new Promise((resolve) => {
    window.requestAnimationFrame(() => resolve());
  });
}

function withTimeout(promise, timeoutMs) {
  if (timeoutMs <= 0) {
    return promise;
  }

  return Promise.race([
    promise,
    new Promise((resolve) => {
      window.setTimeout(resolve, timeoutMs);
    }),
  ]);
}

function waitForEvent(target, eventName, timeoutMs) {
  return withTimeout(
    new Promise((resolve) => {
      const onEvent = () => {
        target.removeEventListener(eventName, onEvent);
        resolve();
      };

      target.addEventListener(eventName, onEvent, { once: true });
    }),
    timeoutMs
  );
}

async function waitForAnimationFrames(count) {
  for (let index = 0; index < count; index += 1) {
    await nextAnimationFrame();
  }
}

function normalizeBuildToken(value) {
  if (typeof value !== "string") {
    return "";
  }

  return value.trim();
}

function buildAppBuildId(buildInfo) {
  if (!buildInfo || typeof buildInfo !== "object") {
    return null;
  }

  const webBuildId = normalizeBuildToken(buildInfo.web_build_id);
  if (webBuildId) {
    return webBuildId;
  }

  const version = normalizeBuildToken(buildInfo.version);
  const buildNumber = normalizeBuildToken(buildInfo.build_number);
  if (!version || !buildNumber) {
    return null;
  }

  return `${version}+${buildNumber}`;
}

async function loadAppBuildInfo() {
  const response = await fetch(APP_BUILD_INFO_URL, {
    cache: "no-store",
    credentials: "same-origin",
  });
  if (!response.ok) {
    throw new Error(`failed to load ${APP_BUILD_INFO_URL}: ${response.status}`);
  }
  return response.json();
}

function withBuildQuery(path, buildId) {
  if (!buildId) {
    return path;
  }

  const url = new URL(path, document.baseURI);
  url.searchParams.set("build", buildId);
  return url.toString();
}

function applyBuildVersionToFlutterEntrypoints(buildId) {
  if (!buildId || !_flutter?.buildConfig?.builds) {
    return;
  }

  for (const build of _flutter.buildConfig.builds) {
    if (typeof build.mainJsPath === "string") {
      build.mainJsPath = withBuildQuery(build.mainJsPath, buildId);
    }
    if (typeof build.mainWasmPath === "string") {
      build.mainWasmPath = withBuildQuery(build.mainWasmPath, buildId);
    }
    if (typeof build.jsSupportRuntimePath === "string") {
      build.jsSupportRuntimePath = withBuildQuery(
        build.jsSupportRuntimePath,
        buildId
      );
    }
  }
}

function schedulePageReload() {
  if (isPageReloadScheduled) {
    return;
  }

  isPageReloadScheduled = true;
  window.location.reload();
}

function requestServiceWorkerActivation(worker) {
  if (!worker) {
    return;
  }

  worker.postMessage({ type: "SKIP_WAITING" });
}

function observeServiceWorkerLifecycle(registration) {
  if (observedServiceWorkerRegistrations.has(registration)) {
    return;
  }
  observedServiceWorkerRegistrations.add(registration);

  registration.addEventListener("updatefound", () => {
    const installingWorker = registration.installing;
    if (!installingWorker) {
      return;
    }

    installingWorker.addEventListener("statechange", () => {
      if (
        installingWorker.state === "installed" &&
        navigator.serviceWorker.controller
      ) {
        requestServiceWorkerActivation(installingWorker);
      }
    });
  });
}

function buildServiceWorkerUrl(buildId) {
  return new URL(withBuildQuery(APP_SERVICE_WORKER_URL, buildId), document.baseURI);
}

async function registerAppServiceWorker(buildId) {
  if (!("serviceWorker" in navigator)) {
    return null;
  }

  try {
    const registration = await navigator.serviceWorker.register(
      buildServiceWorkerUrl(buildId),
      {
        updateViaCache: "none",
      }
    );
    observeServiceWorkerLifecycle(registration);
    if (navigator.serviceWorker.controller && registration.waiting) {
      requestServiceWorkerActivation(registration.waiting);
    }
    return registration;
  } catch (error) {
    console.warn("App service worker registration skipped:", error);
    return null;
  }
}

function startAppUpdateChecks() {
  if (!("serviceWorker" in navigator)) {
    return;
  }

  navigator.serviceWorker.addEventListener("controllerchange", () => {
    schedulePageReload();
  });

  const checkForAppUpdate = async () => {
    try {
      const nextBuildInfo = await withTimeout(
        loadAppBuildInfo(),
        APP_UPDATE_FETCH_TIMEOUT_MS
      );
      const nextBuildId = buildAppBuildId(nextBuildInfo);
      if (!nextBuildId || nextBuildId === currentAppBuildId) {
        return;
      }

      const registration = await registerAppServiceWorker(nextBuildId);
      if (registration) {
        currentAppBuildId = nextBuildId;
      }
    } catch (error) {
      console.warn("App update check skipped:", error);
    }
  };

  window.setInterval(checkForAppUpdate, APP_UPDATE_CHECK_INTERVAL_MS);
  window.addEventListener("focus", () => {
    void checkForAppUpdate();
  });
  document.addEventListener("visibilitychange", () => {
    if (document.visibilityState === "visible") {
      void checkForAppUpdate();
    }
  });
}

const appBuildInfoPromise = (async () => {
  const buildInfo = await withTimeout(
    loadAppBuildInfo().catch((error) => {
      console.warn("App build info fetch skipped:", error);
      return null;
    }),
    APP_UPDATE_FETCH_TIMEOUT_MS
  );
  const buildId = buildAppBuildId(buildInfo);
  if (buildId) {
    currentAppBuildId = buildId;
    applyBuildVersionToFlutterEntrypoints(buildId);
  }
  return buildId;
})();

async function loadFontFallbackManifest() {
  const response = await fetch(FONT_FALLBACK_MANIFEST_URL, {
    credentials: "same-origin",
  });
  if (!response.ok) {
    throw new Error(
      `failed to load ${FONT_FALLBACK_MANIFEST_URL}: ${response.status}`
    );
  }
  return response.json();
}

async function prefetchFont(url) {
  const response = await fetch(url, {
    credentials: "same-origin",
  });
  if (!response.ok) {
    throw new Error(`failed to prefetch ${url}: ${response.status}`);
  }
  await response.arrayBuffer();
}

async function prefetchFontFallbackAssets() {
  const manifest = await loadFontFallbackManifest();
  const baseUrl = manifest.baseUrl || "font-fallbacks/";
  const urls = Array.isArray(manifest.prefetch)
    ? manifest.prefetch.map((path) => `${baseUrl}${path}`)
    : [];

  let nextIndex = 0;
  async function worker() {
    while (nextIndex < urls.length) {
      const url = urls[nextIndex];
      nextIndex += 1;
      await prefetchFont(url);
    }
  }

  const workers = Array.from(
    { length: Math.min(FONT_PREFETCH_CONCURRENCY, urls.length) },
    () => worker()
  );
  await Promise.all(workers);
}

const fontPrefetchPromise = withTimeout(
  prefetchFontFallbackAssets().catch((error) => {
    console.warn("Flutter font fallback prefetch skipped:", error);
  }),
  FONT_PREFETCH_TIMEOUT_MS
);

_flutter.loader.load({
  config: {
    // Keep CanvasKit on the app origin when wasm falls back to it.
    canvasKitBaseUrl: "canvaskit/",
    // Keep Flutter Web fallback fonts on the app origin.
    fontFallbackBaseUrl: "font-fallbacks/",
  },
  onEntrypointLoaded: async function (engineInitializer) {
    const buildId = await appBuildInfoPromise;
    await registerAppServiceWorker(buildId);
    startAppUpdateChecks();
    await fontPrefetchPromise;

    const firstFramePromise = waitForEvent(
      window,
      "flutter-first-frame",
      FIRST_FRAME_TIMEOUT_MS
    );
    const appRunner = await engineInitializer.initializeEngine();
    await appRunner.runApp();

    await firstFramePromise;
    await waitForAnimationFrames(POST_FIRST_FRAME_SETTLE_FRAMES);

    hideBootstrapOverlay();
  },
});
