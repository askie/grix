(function () {
  var script = document.currentScript;
  if (!script) {
    var scripts = document.getElementsByTagName('script');
    script = scripts[scripts.length - 1];
  }
  if (!script) {
    return;
  }

  var siteKey = (script.getAttribute('data-site-key') || '').trim();
  if (!siteKey) {
    console.error('[grix-widget] missing data-site-key');
    return;
  }

  var scriptURL;
  try {
    scriptURL = new URL(script.src, window.location.href);
  } catch (_) {
    return;
  }
  var apiBase = script.getAttribute('data-api-base') || (scriptURL.protocol + '//' + scriptURL.host);

  // 宿主页面若缺 viewport meta，移动端浏览器会用 ~980px 虚拟视口整体缩放渲染，
  // 导致注入的按钮/图标按 CSS px 声明的尺寸被等比缩小("特别特别小")。
  // 按钮和面板都以 px 固定尺寸注入宿主 DOM，无法用子元素 CSS 逆向抵消这个整页缩放，
  // 因此仅在宿主完全没有声明 viewport 时补一条标准 meta，不覆盖宿主已有的设置。
  // 放在 siteKey/scriptURL 校验通过之后：只有挂件确实要渲染时才动宿主页面。
  if (!document.querySelector('meta[name="viewport"]')) {
    var viewportMeta = document.createElement('meta');
    viewportMeta.name = 'viewport';
    viewportMeta.content = 'width=device-width, initial-scale=1';
    document.head.appendChild(viewportMeta);
  }

  // Local data-* overrides — null means "not set by site owner", use server config instead.
  var localButtonLabel = script.getAttribute('data-button-label');
  var localWelcome     = script.getAttribute('data-welcome');
  var localThemeColor  = script.getAttribute('data-theme-color');
  var localPosition    = script.getAttribute('data-position');
  var localAutoExpand  = script.getAttribute('data-auto-expand');
  var localTitle       = script.getAttribute('data-title');
  // Optional force locale (e.g. zh_CN / en_US). Empty = follow browser language.
  var localLocale      = (script.getAttribute('data-locale') || '').trim();

  function resolveLocale() {
    return localLocale || navigator.language || 'en-US';
  }

  var visitorStorageKey = 'grix_widget_visitor_key_' + siteKey;
  var visitorKey = localStorage.getItem(visitorStorageKey) || '';

  // Resolved config — filled once server display_config is received.
  var resolvedPosition = localPosition || 'right';

  var root = document.createElement('div');
  root.style.position = 'fixed';
  root.style.bottom = '20px';
  root.style.zIndex = '2147483000';
  root.style[resolvedPosition === 'left' ? 'left' : 'right'] = '20px';

  var button = document.createElement('button');
  button.type = 'button';
  button.textContent = localButtonLabel || 'Chat';
  button.style.width = '58px';
  button.style.height = '58px';
  button.style.border = 'none';
  button.style.borderRadius = '999px';
  button.style.background = localThemeColor || '#0f766e';
  button.style.color = '#ffffff';
  button.style.fontSize = '13px';
  button.style.fontWeight = '700';
  button.style.cursor = 'pointer';
  button.style.boxShadow = '0 12px 30px rgba(0,0,0,0.2)';

  var panel = document.createElement('div');
  panel.style.width = '360px';
  panel.style.height = '560px';
  panel.style.maxWidth = 'calc(100vw - 24px)';
  panel.style.maxHeight = 'calc(100vh - 90px)';
  panel.style.border = '1px solid #d4d4d8';
  panel.style.borderRadius = '16px';
  panel.style.overflow = 'hidden';
  panel.style.background = '#fff';
  panel.style.boxShadow = '0 18px 45px rgba(0,0,0,0.24)';
  panel.style.marginBottom = '12px';
  panel.style.display = 'none';

  var iframe = document.createElement('iframe');
  iframe.title = 'Grix Chat Widget';
  iframe.style.width = '100%';
  iframe.style.height = '100%';
  iframe.style.border = '0';
  iframe.referrerPolicy = 'origin';
  iframe.allow = 'microphone';

  panel.appendChild(iframe);
  root.appendChild(panel);
  root.appendChild(button);
  document.body.appendChild(root);

  var inited = false;
  var refreshTimer = null;

  function openPanel() {
    panel.style.display = 'block';
    button.style.display = 'none';
  }

  window.addEventListener('message', function (event) {
    if (!event || !event.data) return;
    if (event.data.type === 'grix_widget_close' || event.data.type === 'grix_widget_session_closed') {
      panel.style.display = 'none';
      button.style.display = 'block';
    }
    // 方案二兜底：iframe 鉴权失败时主动触发重新 init
    if (event.data.type === 'grix_widget_need_refresh') {
      reInitWidget();
    }
  });

  button.addEventListener('click', function () {
    var opening = panel.style.display === 'none';
    panel.style.display = opening ? 'block' : 'none';
    button.style.display = opening ? 'none' : 'block';
    if (opening && !inited) {
      initWidget();
    }
  });

  function scheduleTokenRefresh(expiresIn) {
    if (refreshTimer) clearTimeout(refreshTimer);
    // 在到期前 5 分钟主动刷新，最少 10 秒后触发
    var delay = Math.max((expiresIn - 300) * 1000, 10000);
    refreshTimer = setTimeout(function () {
      refreshTimer = null;
      reInitWidget();
    }, delay);
  }

  function reInitWidget() {
    if (!inited) return;
    var pageURL = window.location.href;
    fetch(apiBase + '/v1/widget/visitor/init', {
      method: 'POST',
      credentials: 'omit',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        site_key: siteKey,
        visitor_key: visitorKey,
        page_url: pageURL,
        locale: resolveLocale()
      })
    }).then(function (resp) {
      if (!resp.ok) throw new Error('refresh failed: ' + resp.status);
      return resp.json();
    }).then(function (raw) {
      var data = raw && raw.data ? raw.data : raw;
      if (!data || !data.widget_token) throw new Error('invalid refresh response');
      visitorKey = data.visitor_key || visitorKey;
      if (visitorKey) localStorage.setItem(visitorStorageKey, visitorKey);
      // 把新 token 推给 iframe
      if (iframe.contentWindow) {
        iframe.contentWindow.postMessage({
          type: 'grix_widget_token_refresh',
          payload: { token: data.widget_token }
        }, '*');
      }
      // 安排下一次主动刷新
      if (data.expires_in) scheduleTokenRefresh(data.expires_in);
    }).catch(function (err) {
      console.error('[grix-widget] token refresh error', err);
      // 刷新失败，60 秒后重试
      refreshTimer = setTimeout(function () { refreshTimer = null; reInitWidget(); }, 60000);
    });
  }

  // Merge server display_config with local data-* overrides.
  // data-* attributes take precedence when explicitly set by the site owner.
  function mergeConfig(serverCfg) {
    serverCfg = serverCfg || {};
    return {
      theme_color:  localThemeColor  || serverCfg.theme_color  || '#0f766e',
      button_label: localButtonLabel || serverCfg.button_label || 'Chat',
      welcome:      localWelcome     != null ? localWelcome : (serverCfg.welcome || ''),
      position:     localPosition    || serverCfg.position    || 'right',
      auto_expand:  localAutoExpand != null ? (localAutoExpand === 'true') : (serverCfg.auto_expand === true),
      title:        localTitle       || serverCfg.title        || '',
    };
  }

  function applyConfig(cfg) {
    // Update button label + color in case they changed after initial render.
    button.textContent = cfg.button_label;
    button.style.background = cfg.theme_color;

    // Reposition root if position changed.
    var pos = cfg.position === 'left' ? 'left' : 'right';
    var opposite = pos === 'left' ? 'right' : 'left';
    root.style[pos] = '20px';
    root.style[opposite] = '';
  }

  function initWidget() {
    inited = true;
    var pageURL = window.location.href;
    var origin = window.location.origin;

    fetch(apiBase + '/v1/widget/visitor/init', {
      method: 'POST',
      credentials: 'omit',
      headers: {
        'Content-Type': 'application/json'
      },
      body: JSON.stringify({
        site_key: siteKey,
        visitor_key: visitorKey,
        page_url: pageURL,
        locale: resolveLocale()
      })
    }).then(function (resp) {
      if (!resp.ok) {
        throw new Error('init failed: ' + resp.status);
      }
      return resp.json();
    }).then(function (raw) {
      var data = raw && raw.data ? raw.data : raw;
      if (!data || !data.session_id || !data.widget_token || !data.ws_url) {
        throw new Error('invalid init response');
      }
      visitorKey = data.visitor_key || visitorKey;
      if (visitorKey) {
        localStorage.setItem(visitorStorageKey, visitorKey);
      }

      var cfg = mergeConfig(data.display_config);
      applyConfig(cfg);

      var frameURL = apiBase + '/public/widget/frame.html';
      iframe.src = frameURL;
      iframe.onload = function () {
        iframe.contentWindow.postMessage({
          type: 'grix_widget_bootstrap',
          payload: {
            site_key: siteKey,
            session_id: data.session_id,
            visitor_id: data.visitor_id,
            token: data.widget_token,
            ws_url: data.ws_url,
            theme_color: cfg.theme_color,
            welcome: cfg.welcome,
            title: cfg.title,
            host_origin: origin,
            voice_enabled: data.voice_enabled === true,
            // 服务端按访客 locale 归一化后的语言，供弹窗自身 UI 文案(frame.html)多语言使用
            locale: data.resolved_locale || 'en_US'
          }
        }, '*');
        // 方案一：bootstrap 后安排主动刷新
        if (data.expires_in) scheduleTokenRefresh(data.expires_in);
      };

      // auto_expand: open panel automatically after init completes
      if (cfg.auto_expand) {
        openPanel();
      }
    }).catch(function (err) {
      inited = false;
      panel.style.display = 'none';
      button.style.display = 'block';
      console.error('[grix-widget] init error', err);
    });
  }

  // On page load, fetch the public display config (no session created) so we can
  // apply appearance to the launcher button and decide whether to auto-expand.
  // data-* attributes still win over server config.
  function bootstrapConfig() {
    // If the owner forced auto-expand via attribute, open immediately without waiting.
    if (localAutoExpand === 'true') {
      openPanel();
      initWidget();
      return;
    }
    fetch(apiBase + '/v1/widget/config?site_key=' + encodeURIComponent(siteKey) +
      '&locale=' + encodeURIComponent(resolveLocale()), {
      method: 'GET',
      credentials: 'omit'
    }).then(function (resp) {
      if (!resp.ok) throw new Error('config failed: ' + resp.status);
      return resp.json();
    }).then(function (raw) {
      var data = raw && raw.data ? raw.data : raw;
      var serverCfg = (data && data.display_config) || {};
      var cfg = mergeConfig(serverCfg);
      applyConfig(cfg);
      if (cfg.auto_expand) {
        openPanel();
        initWidget();
      }
    }).catch(function () {
      // Config fetch is best-effort; launcher still works with local/default config.
    });
  }

  bootstrapConfig();
})();
