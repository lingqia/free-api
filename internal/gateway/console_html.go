package gateway

// consoleHTML 是内置控制台页面，遵循 Notion 风格：
// 米色底 #f7f6f3、深灰正文 #37352f、细边框、克制悬停，无圆角过大、无渐变、无位移动画。
const consoleHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Free API 网关</title>
<style>
  :root {
    --notion-text: #37352f;
    --notion-text-gray: #9b9a97;
    --notion-bg: #ffffff;
    --notion-bg-gray: #f7f6f3;
    --notion-hover: #efedea;
    --notion-active: #e3e1db;
    --notion-blue: #2eaadc;
    --notion-red: #eb5757;
    --notion-green: #0f7b6c;
    --notion-yellow: #dfab01;
    --notion-border: rgba(55, 53, 47, 0.09);
  }
  * { box-sizing: border-box; }
  body {
    margin: 0;
    font-family: ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
    background: var(--notion-bg-gray);
    color: var(--notion-text);
    line-height: 1.5;
    font-size: 14px;
  }
  .layout { display: flex; min-height: 100vh; }
  .sidebar {
    width: 220px;
    flex-shrink: 0;
    background: var(--notion-bg-gray);
    border-right: 1px solid var(--notion-border);
    padding: 16px 8px;
  }
  .brand {
    font-weight: 600;
    font-size: 15px;
    padding: 6px 10px;
    margin-bottom: 12px;
    color: var(--notion-text);
  }
  .nav-item {
    display: block;
    width: 100%;
    text-align: left;
    padding: 6px 10px;
    border: none;
    background: transparent;
    color: var(--notion-text);
    font-size: 14px;
    border-radius: 4px;
    cursor: pointer;
    transition: background-color 150ms;
  }
  .nav-item:hover { background: var(--notion-hover); }
  .nav-item.active { background: var(--notion-active); font-weight: 500; }
  .main { flex: 1; padding: 32px 40px; max-width: 1800px; margin: 0 auto; }
  h1 { font-size: 28px; font-weight: 600; margin: 0 0 4px; }
  .sub { color: var(--notion-text-gray); font-size: 13px; margin-bottom: 24px; }
  h2 { font-size: 18px; font-weight: 600; margin: 0 0 12px; }
  .card {
    background: var(--notion-bg);
    border: 1px solid var(--notion-border);
    border-radius: 8px;
    padding: 16px;
    margin-bottom: 12px;
  }
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 10px; }
  .card-head h2 { margin: 0; }
  .pager { display: flex; align-items: center; justify-content: flex-end; gap: 8px; margin-top: 10px; font-size: 12px; color: var(--notion-text-gray); }
  .pager .btn { padding: 2px 8px; font-size: 12px; }
  .pager.hidden { display: none; }
  .row { display: flex; gap: 8px; flex-wrap: wrap; align-items: center; }
  .between { justify-content: space-between; }
  .badge {
    display: inline-block;
    font-size: 12px;
    padding: 1px 6px;
    border-radius: 4px;
    border: 1px solid var(--notion-border);
    background: var(--notion-bg-gray);
    color: var(--notion-text);
  }
  .badge.ok { color: var(--notion-green); background: #f0f9f6; border-color: #d3ebe4; }
  .badge.warn { color: #8a6a00; background: #fdf7e5; border-color: #f0e4bd; }
  .badge.err { color: var(--notion-red); background: #fdf2f2; border-color: #f3d5d5; }
  .muted { color: var(--notion-text-gray); font-size: 12px; }
  button.btn {
    border: 1px solid var(--notion-border);
    background: transparent;
    color: var(--notion-text);
    font-size: 13px;
    font-weight: 500;
    padding: 4px 10px;
    border-radius: 6px;
    cursor: pointer;
    transition: background-color 150ms;
  }
  button.btn:hover { background: var(--notion-hover); }
  button.btn:active { background: var(--notion-active); }
  button.btn.primary { background: var(--notion-blue); color: #fff; border-color: transparent; }
  input[type=text], input[type=password], textarea {
    width: 100%;
    border: 0;
    border-bottom: 1px solid var(--notion-border);
    background: transparent;
    color: var(--notion-text);
    font-size: 14px;
    padding: 6px 0;
    outline: none;
    transition: border-color 150ms;
    font-family: inherit;
  }
  input:focus, textarea:focus { border-bottom-color: var(--notion-blue); }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 12px; }
  .toolbar { margin-bottom: 16px; }
  .kv { display: flex; gap: 16px; flex-wrap: wrap; margin-top: 8px; }
  .kv div { font-size: 12px; color: var(--notion-text-gray); }
  .kv b { color: var(--notion-text); font-weight: 600; }
  pre {
    background: rgba(135,131,120,0.15);
    border-radius: 4px;
    padding: 10px;
    font-size: 12px;
    overflow-x: auto;
    font-family: SFMono-Regular, Menlo, Consolas, monospace;
  }
  .hidden { display: none; }
  .endpoint { font-family: SFMono-Regular, Menlo, Consolas, monospace; font-size: 12px; color: var(--notion-text); }
  .groups { display: flex; gap: 8px; flex-wrap: wrap; }

  /* 列表页工具栏：搜索 + 按钮 */
  .site-toolbar, .model-toolbar { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; flex-wrap: wrap; }
  .search-input {
    flex: 1;
    min-width: 220px;
    max-width: 760px;
    border: 1px solid var(--notion-border);
    border-radius: 6px;
    background: var(--notion-bg);
    padding: 8px 10px;
    font-family: inherit;
    font-size: 14px;
    color: var(--notion-text);
    outline: none;
  }
  .search-input:focus { border-color: var(--notion-blue); }
  .toolbar-summary { margin-left: auto; }
  .site-grid { grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); }
  @media (min-width: 1600px) {
    .site-grid { grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); }
  }
  .plugin-card {
    margin-bottom: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
    cursor: pointer;
  }
  .plugin-card:hover { border-color: rgba(55, 53, 47, 0.22); background: #fcfcfa; }
  .plugin-card:focus-visible { outline: 2px solid var(--notion-blue); outline-offset: 2px; }
  .plugin-head { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; }
  .plugin-name { font-size: 15px; font-weight: 600; }
  .plugin-detail-hint { margin-top: auto; text-align: right; font-size: 11px; color: var(--notion-text-gray); }

  /* 卡片统计（图二布局） */
  .stat-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 7px 16px; }
  .stat { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; min-width: 0; font-size: 12px; }
  .stat-label { color: var(--notion-text-gray); white-space: nowrap; }
  .stat-value { font-weight: 600; font-variant-numeric: tabular-nums; text-align: right; }
  .stat.wide { grid-column: 1 / -1; }

  /* 列表/表格：模型目录与 API Key 共用 */
  .table-wrap {
    border: 1px solid var(--notion-border);
    border-radius: 8px;
    overflow: hidden;
    background: var(--notion-bg);
  }
  .data-table { width: 100%; border-collapse: collapse; }
  .data-table th, .data-table td {
    text-align: left;
    padding: 9px 12px;
    border-bottom: 1px solid var(--notion-border);
    font-size: 13px;
    vertical-align: middle;
  }
  .data-table th {
    font-size: 12px;
    font-weight: 500;
    color: var(--notion-text-gray);
    background: var(--notion-bg-gray);
    white-space: nowrap;
  }
  .data-table tbody tr:last-child td { border-bottom: none; }
  .data-table tbody tr:hover { background: #fcfcfa; }
  .data-table .mono {
    font-family: SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .data-table .num { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
  .source-list { display: flex; flex-wrap: wrap; gap: 4px; }
  .table-empty { padding: 18px; text-align: center; color: var(--notion-text-gray); font-size: 13px; }
  .table-actions { display: flex; gap: 6px; justify-content: flex-end; white-space: nowrap; }
  /* 模型目录：站点分组可折叠 */
  .site-group { margin-bottom: 10px; }
  .site-group-head {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    background: var(--notion-bg);
    border: 1px solid var(--notion-border);
    border-radius: 8px;
    cursor: pointer;
    user-select: none;
  }
  .site-group-head:hover { background: #fcfcfa; }
  .site-group-head .chevron { color: var(--notion-text-gray); font-size: 11px; width: 12px; transition: transform 150ms; }
  .site-group-head .name { font-weight: 600; font-size: 14px; }
  .site-group-head .count { margin-left: auto; color: var(--notion-text-gray); font-size: 12px; }
  .site-group-body { margin-top: 6px; }
  .site-group.collapsed .site-group-body { display: none; }
  .site-group.collapsed .site-group-head .chevron { transform: rotate(-90deg); }
  .model-chip {
    display: inline-flex;
    align-items: center;
    max-width: 100%;
    border: 1px solid var(--notion-border);
    background: var(--notion-bg-gray);
    border-radius: 4px;
    padding: 2px 8px;
    font-family: SFMono-Regular, Menlo, Consolas, monospace;
    font-size: 11px;
    overflow-wrap: anywhere;
  }
  .model-chip.off { opacity: .55; }

  /* 站点详情弹层 */
  .overlay {
    position: fixed;
    inset: 0;
    z-index: 50;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: 40px 20px;
    background: rgba(20, 20, 20, 0.28);
    overflow-y: auto;
  }
  .overlay.hidden { display: none; }
  .detail-modal {
    width: min(820px, 100%);
    background: var(--notion-bg);
    border: 1px solid var(--notion-border);
    border-radius: 10px;
    padding: 24px;
  }
  .detail-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 18px; }
  .detail-title { font-size: 22px; font-weight: 600; margin: 0 0 6px; }
  .detail-section { margin-top: 24px; }
  .detail-section h3 { font-size: 14px; font-weight: 600; margin: 0 0 10px; }
  .detail-models { display: flex; flex-wrap: wrap; gap: 6px; max-height: 220px; overflow-y: auto; }
  .detail-note { margin-bottom: 12px; line-height: 1.6; }
  .detail-stat-grid { grid-template-columns: repeat(3, 1fr); gap: 10px 18px; }
  .cred-field { margin-bottom: 16px; }
  .cred-field label { display: block; font-size: 12px; color: var(--notion-text-gray); margin-bottom: 6px; }
  .cred-field textarea {
    width: 100%;
    min-height: 64px;
    border: 1px solid var(--notion-border);
    border-radius: 6px;
    padding: 8px 10px;
    font-size: 12px;
    font-family: SFMono-Regular, Menlo, Consolas, monospace;
    resize: vertical;
    background: var(--notion-bg);
    color: var(--notion-text);
    outline: none;
  }
  .cred-field textarea:focus { border-color: var(--notion-blue); }
  .cred-actions { display: flex; align-items: center; justify-content: flex-end; gap: 8px; margin-top: 6px; }

  /* 代理池 */
  .proxy-list { display: flex; flex-wrap: wrap; gap: 6px; min-height: 24px; }

  /* 设置 */
  .settings-form { display: grid; grid-template-columns: 90px minmax(0, 420px); gap: 10px 12px; align-items: center; }
  .settings-form label { font-size: 12px; color: var(--notion-text-gray); }
  .settings-textarea {
    width: 100%;
    min-height: 120px;
    border: 1px solid var(--notion-border);
    border-radius: 6px;
    padding: 8px 10px;
    font-size: 12px;
    font-family: SFMono-Regular, Menlo, Consolas, monospace;
    resize: vertical;
    background: var(--notion-bg);
    color: var(--notion-text);
    outline: none;
  }
  .settings-textarea:focus { border-color: var(--notion-blue); }
  .form-actions { display: flex; justify-content: flex-end; align-items: center; gap: 8px; margin-top: 12px; }
  button.btn.danger { color: var(--notion-red); border-color: #f3d5d5; }
  button.btn.danger:hover { background: #fdf2f2; }

  /* 登录遮罩 */
  .login-overlay {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;
    background: var(--notion-bg-gray);
  }
  .login-overlay.hidden { display: none; }
  .login-card {
    width: min(360px, 100%);
    background: var(--notion-bg);
    border: 1px solid var(--notion-border);
    border-radius: 10px;
    padding: 24px;
  }
  .login-brand { font-size: 18px; font-weight: 600; margin-bottom: 6px; }
  #login-password {
    border: 1px solid var(--notion-border);
    border-radius: 6px;
    padding: 8px 10px;
    background: var(--notion-bg);
  }
  #login-password:focus { border-color: var(--notion-blue); }

  /* 自定义弹窗与轻提示 */
  .dialog-overlay {
    position: fixed;
    inset: 0;
    z-index: 120;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;
    background: rgba(20, 20, 20, 0.28);
  }
  .dialog-overlay.hidden { display: none; }
  .dialog {
    width: min(420px, 100%);
    background: var(--notion-bg);
    border: 1px solid var(--notion-border);
    border-radius: 10px;
    padding: 20px 24px;
    box-shadow: 0 12px 32px rgba(0, 0, 0, 0.12);
  }
  .dialog-title { font-size: 16px; font-weight: 600; margin-bottom: 8px; }
  .dialog-message { font-size: 13px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 20px; }
  .toast-wrap {
    position: fixed;
    top: 20px;
    right: 20px;
    z-index: 130;
    display: flex;
    flex-direction: column;
    gap: 8px;
    pointer-events: none;
  }
  .toast {
    background: var(--notion-text);
    color: #fff;
    font-size: 13px;
    padding: 8px 14px;
    border-radius: 8px;
    box-shadow: 0 6px 16px rgba(0, 0, 0, 0.18);
  }
  .toast.err { background: var(--notion-red); }
  @media (max-width: 720px) {
    .detail-stat-grid { grid-template-columns: 1fr 1fr; }
  }
</style>
</head>
<body>
<div class="layout">
  <aside class="sidebar">
    <div class="brand">Free API 网关</div>
    <button class="nav-item active" data-tab="plugins">站点插件</button>
    <button class="nav-item" data-tab="models">模型目录</button>
    <button class="nav-item" data-tab="proxy">代理池</button>
    <button class="nav-item" data-tab="usage">接口说明</button>
    <button class="nav-item" data-tab="settings">设置</button>
  </aside>
  <main class="main">
    <section id="tab-plugins">
      <h1>站点插件</h1>
      <div class="sub">每个站点是一个独立插件进程。工具档位决定它能否承接带工具的请求。</div>
      <div class="toolbar site-toolbar">
        <input id="plugin-search" class="search-input" type="text" placeholder="搜索站点或模型" onkeydown="if(event.key==='Enter')applyPluginSearch()">
        <button class="btn" onclick="applyPluginSearch()">搜索</button>
        <button class="btn" onclick="clearPluginSearch()">清除</button>
        <button class="btn" onclick="loadPlugins()">刷新</button>
        <span class="muted toolbar-summary" id="plugin-summary"></span>
      </div>
      <div id="plugins"></div>
    </section>

    <section id="tab-models" class="hidden">
      <h1>模型目录</h1>
      <div class="sub">按站点分组展示模型，站点可收起/展开；支持按模型或站点搜索。</div>
      <div class="toolbar model-toolbar">
        <input id="model-search" class="search-input" type="text" placeholder="搜索模型或站点" onkeydown="if(event.key==='Enter')applyModelSearch()">
        <button class="btn" onclick="applyModelSearch()">搜索</button>
        <button class="btn" onclick="clearModelSearch()">清除</button>
        <button class="btn" onclick="loadModels()">刷新</button>
        <button class="btn" id="model-toggle-all" onclick="toggleAllSites()">全部收起</button>
        <span class="muted toolbar-summary" id="model-summary"></span>
      </div>
      <div id="models"></div>
      <div class="pager hidden" id="model-pager"></div>
    </section>

    <section id="tab-proxy" class="hidden">
      <h1>代理池</h1>
      <div class="sub">所有站点共用此代理池，按调用轮换；站点不再单独配置代理。</div>
      <div class="card">
        <div class="card-head">
          <h2>当前代理</h2>
          <div class="row">
            <span class="muted" id="proxy-summary">加载中…</span>
            <button class="btn danger" onclick="clearProxyPool()">清空</button>
            <button class="btn primary" onclick="openProxyModal()">添加代理</button>
          </div>
        </div>
        <div id="proxy-list"></div>
        <div class="pager hidden" id="proxy-pager"></div>
        <div class="muted detail-note" style="margin-top:12px">支持 http、https、socks5；不写协议时按 http 处理。匿名站点的账号数会按代理数量计算，所有站点调用都会轮换使用。</div>
      </div>
    </section>

    <section id="tab-usage" class="hidden">
      <h1>接口说明</h1>
      <div class="sub">三套协议共用同一套插件，可混用。</div>
      <div class="card">
        <h2>OpenAI Chat Completions</h2>
        <div class="endpoint">POST /v1/chat/completions</div>
        <pre>curl http://localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"echo-mini","messages":[{"role":"user","content":"hi"}],"stream":true}'</pre>
      </div>
      <div class="card">
        <h2>OpenAI Responses</h2>
        <div class="endpoint">POST /v1/responses</div>
        <pre>curl http://localhost:8080/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{"model":"echo-mini","input":"hi","stream":true}'</pre>
      </div>
      <div class="card">
        <h2>Anthropic Messages</h2>
        <div class="endpoint">POST /v1/messages</div>
        <pre>curl http://localhost:8080/v1/messages \
  -H 'Content-Type: application/json' \
  -d '{"model":"echo-mini","max_tokens":256,"messages":[{"role":"user","content":"hi"}],"stream":true}'</pre>
      </div>
      <div class="card">
        <h2>模型与能力</h2>
        <div class="endpoint">GET /v1/models</div>
        <div class="muted">每个模型带 capabilities.tools（native / builtin / none）与 auth 状态。</div>
      </div>
    </section>

    <section id="tab-settings" class="hidden">
      <h1>设置</h1>
      <div class="sub">控制台登录密码与网关访问 Key。设置会写入本地配置文件。</div>
      <div class="card">
        <h2>控制台登录密码</h2>
        <div class="muted" style="margin-bottom:12px">设置后打开控制台需要登录；清除密码则恢复免登录。</div>
        <div class="settings-form">
          <label for="settings-password">新密码</label>
          <input type="password" id="settings-password" autocomplete="new-password">
          <label for="settings-password2">确认密码</label>
          <input type="password" id="settings-password2" autocomplete="new-password">
        </div>
        <div class="row between" style="margin-top:12px">
          <span class="muted" id="settings-password-state"></span>
          <div class="row">
            <button class="btn" onclick="clearPassword()">清除密码</button>
            <button class="btn primary" onclick="savePassword()">保存密码</button>
          </div>
        </div>
      </div>
      <div class="card">
        <div class="card-head">
          <h2>网关 API Key</h2>
          <button class="btn primary" onclick="openKeyModal()">添加 Key</button>
        </div>
        <div class="muted" style="margin-bottom:12px">用于 /v1 接口鉴权；停用的 Key 立即失效。调用次数统计自网关本次启动。</div>
        <div id="settings-keys-table"></div>
        <div class="pager hidden" id="keys-pager"></div>
      </div>
      <div class="card">
        <h2>控制台会话</h2>
        <div class="row">
          <button class="btn" onclick="logout()">退出登录</button>
          <span class="muted">退出后需要重新输入控制台密码。</span>
        </div>
      </div>
    </section>
  </main>
</div>

<div id="detail-overlay" class="overlay hidden">
  <div class="detail-modal" role="dialog" aria-modal="true" aria-labelledby="detail-title">
    <div class="detail-head">
      <div>
        <h2 class="detail-title" id="detail-title"></h2>
        <div class="row" id="detail-badges"></div>
      </div>
      <button class="btn" onclick="closeDetail()">关闭</button>
    </div>
    <div id="detail-body"></div>
  </div>
</div>

<div id="login-overlay" class="login-overlay hidden">
  <div class="login-card">
    <div class="login-brand">Free API 网关</div>
    <div class="muted" style="margin-bottom:16px">请输入控制台登录密码</div>
    <input type="password" id="login-password" placeholder="登录密码" autocomplete="current-password">
    <div class="row" style="margin-top:16px; justify-content:flex-end">
      <button class="btn primary" onclick="doLogin()">登录</button>
    </div>
    <div class="muted" id="login-error" style="color:var(--notion-red); margin-top:10px; min-height:18px"></div>
  </div>
</div>

<div id="dialog-overlay" class="dialog-overlay hidden">
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="dialog-title">
    <div class="dialog-title" id="dialog-title"></div>
    <div class="dialog-message" id="dialog-message"></div>
    <div class="dialog-actions">
      <button class="btn hidden" id="dialog-cancel" onclick="closeDialog(false)">取消</button>
      <button class="btn primary" id="dialog-ok" onclick="closeDialog(true)">确定</button>
    </div>
  </div>
</div>
<div id="key-modal" class="dialog-overlay hidden" style="z-index:110">
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="key-modal-title">
    <div class="dialog-title" id="key-modal-title">添加 API Key</div>
    <div class="muted" style="margin-bottom:10px">每行一个 Key，可一次粘贴多个；已存在的 Key 会自动忽略。</div>
    <textarea id="key-modal-input" class="settings-textarea" placeholder="sk-..."></textarea>
    <div class="dialog-actions">
      <button class="btn" onclick="closeKeyModal()">取消</button>
      <button class="btn primary" onclick="submitKeys()">添加</button>
    </div>
  </div>
</div>

<div id="proxy-modal" class="dialog-overlay hidden" style="z-index:110">
  <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="proxy-modal-title">
    <div class="dialog-title" id="proxy-modal-title">添加代理</div>
    <div class="muted" style="margin-bottom:10px">每行一个代理，支持 http、https、socks5；不写协议按 http 处理。</div>
    <textarea id="proxy-modal-input" class="settings-textarea" placeholder="http://127.0.0.1:7890&#10;socks5://127.0.0.1:1080"></textarea>
    <div class="dialog-actions">
      <button class="btn" onclick="closeProxyModal()">取消</button>
      <button class="btn primary" onclick="submitProxies()">添加</button>
    </div>
  </div>
</div>

<div id="toast-wrap" class="toast-wrap"></div>

<script>
const state = {
  plugins: [],
  models: [],
  pluginQuery: '',
  modelQuery: '',
  detailName: '',
  locked: false,
  proxy: { count: 0, proxies: [] },
  settings: { password_set: false, keys: [] },
  keyPage: 1,
  proxyPage: 1,
  modelPage: 1,
  collapsedSites: {},
  allCollapsed: false
};
if ('scrollRestoration' in history) history.scrollRestoration = 'manual';

const TAB_NAMES = ['plugins', 'models', 'proxy', 'usage', 'settings'];
const TAB_STORAGE_KEY = 'freeapi.activeTab';
const SCROLL_STORAGE_KEY = 'freeapi.scrollY';

function activateTab(tab, remember) {
  if (TAB_NAMES.indexOf(tab) < 0) tab = 'plugins';
  document.querySelectorAll('.nav-item').forEach(b => {
    b.classList.toggle('active', b.dataset.tab === tab);
  });
  document.querySelectorAll('main > section').forEach(s => s.classList.add('hidden'));
  const section = document.getElementById('tab-' + tab);
  if (section) section.classList.remove('hidden');
  if (remember) {
    try { localStorage.setItem(TAB_STORAGE_KEY, tab); } catch (e) {}
    if (location.hash !== '#' + tab) history.replaceState(null, '', '#' + tab);
  }
}

document.querySelectorAll('.nav-item').forEach(btn => {
  btn.addEventListener('click', () => {
    activateTab(btn.dataset.tab, true);
    window.scrollTo(0, 0);
    try { localStorage.setItem(SCROLL_STORAGE_KEY, '0'); } catch (e) {}
  });
});

let scrollSaveTimer = null;
window.addEventListener('scroll', () => {
  if (scrollSaveTimer) clearTimeout(scrollSaveTimer);
  scrollSaveTimer = setTimeout(() => {
    try { localStorage.setItem(SCROLL_STORAGE_KEY, String(window.scrollY)); } catch (e) {}
  }, 200);
});

let initialTab = (location.hash || '').replace('#', '');
if (TAB_NAMES.indexOf(initialTab) < 0) {
  try { initialTab = localStorage.getItem(TAB_STORAGE_KEY) || 'plugins'; } catch (e) { initialTab = 'plugins'; }
}
activateTab(initialTab, true);

function el(tag, cls, text) {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function badge(text, kind) {
  return el('span', 'badge' + (kind ? ' ' + kind : ''), text);
}

let dialogResolve = null;

// openDialog 显示自定义弹窗，返回 Promise<boolean>（确定为 true）。
function openDialog(opts) {
  return new Promise(function (resolve) {
    dialogResolve = resolve;
    document.getElementById('dialog-title').textContent = opts.title || '提示';
    document.getElementById('dialog-message').textContent = opts.message || '';
    const ok = document.getElementById('dialog-ok');
    ok.textContent = opts.okText || '确定';
    ok.className = 'btn ' + (opts.danger ? 'danger' : 'primary');
    document.getElementById('dialog-cancel').classList.toggle('hidden', !opts.confirm);
    document.getElementById('dialog-overlay').classList.remove('hidden');
    setTimeout(function () { ok.focus(); }, 30);
  });
}

// closeDialog 关闭弹窗并把结果交还给 openDialog 的调用方。
function closeDialog(result) {
  document.getElementById('dialog-overlay').classList.add('hidden');
  const resolve = dialogResolve;
  dialogResolve = null;
  if (resolve) resolve(result);
}

// uiAlert 显示只有确定按钮的提示弹窗。
function uiAlert(message, title) {
  return openDialog({ title: title || '提示', message: message });
}

// uiConfirm 显示带取消按钮的确认弹窗。
function uiConfirm(message, opts) {
  return openDialog(Object.assign({ title: '确认操作', message: message, confirm: true }, opts || {}));
}

// toast 在右上角显示短暂提示。
function toast(message, kind) {
  const wrap = document.getElementById('toast-wrap');
  if (!wrap) return;
  const item = el('div', 'toast' + (kind === 'err' ? ' err' : ''), message);
  wrap.appendChild(item);
  setTimeout(function () {
    item.style.transition = 'opacity 200ms';
    item.style.opacity = '0';
    setTimeout(function () { item.remove(); }, 220);
  }, 2200);
}

function toolsLabel(t) {
  if (t === 'native') return ['支持工具透传', 'ok'];
  if (t === 'builtin') return ['仅平台内置工具', 'warn'];
  return ['无法使用工具', 'err'];
}

// apiJSON 请求 JSON 接口；会话失效时自动弹出登录遮罩并返回 null。
async function apiJSON(url, opts) {
  const res = await fetch(url, opts);
  if (res.status === 401) { showLogin(); return null; }
  if (!res.ok) throw new Error('HTTP ' + res.status);
  return res.json();
}

// apiSend 发送不需要读取响应体的接口请求。
async function apiSend(url, opts) {
  const res = await fetch(url, opts);
  if (res.status === 401) { showLogin(); return false; }
  return res.ok;
}

function showLogin() {
  state.locked = true;
  if (state.detailName) closeDetail();
  const overlay = document.getElementById('login-overlay');
  if (!overlay) return;
  overlay.classList.remove('hidden');
  document.body.style.overflow = 'hidden';
  const error = document.getElementById('login-error');
  if (error) error.textContent = '';
  const input = document.getElementById('login-password');
  if (input) {
    input.value = '';
    setTimeout(function () { input.focus(); }, 50);
  }
}

function hideLogin() {
  state.locked = false;
  const overlay = document.getElementById('login-overlay');
  if (overlay) overlay.classList.add('hidden');
  document.body.style.overflow = '';
}

async function doLogin() {
  const input = document.getElementById('login-password');
  const error = document.getElementById('login-error');
  if (error) error.textContent = '';
  const res = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: input ? input.value : '' })
  });
  if (!res.ok) {
    let msg = '登录失败';
    try {
      const data = await res.json();
      if (data && data.error && data.error.message) msg = data.error.message;
    } catch (e) {}
    if (error) error.textContent = msg;
    return;
  }
  hideLogin();
  await initData();
}

async function logout() {
  await fetch('/api/logout', { method: 'POST' });
  let session = null;
  try { session = await (await fetch('/api/session')).json(); } catch (e) {}
  if (session && session.password_set) {
    state.settings.password_set = true;
    showLogin();
  } else {
    location.reload();
  }
}

async function loadPlugins() {
  const data = await apiJSON('/api/plugins');
  if (!data) return;
  state.plugins = data.plugins || [];
  renderPlugins();
  if (state.detailName) {
    const p = state.plugins.find(x => x.name === state.detailName);
    if (p) updateDetailHeader(p);
    else closeDetail();
  }
}

function fmtNum(v) {
  return String(v === undefined || v === null ? 0 : v);
}

function successRate(stats) {
  if (!stats) return '—';
  const done = (stats.success_calls || 0) + (stats.failed_calls || 0);
  if (!done) return '—';
  return Math.round(stats.success_rate || 0) + '%';
}

function statRow(label, value, wide) {
  const row = el('div', 'stat' + (wide ? ' wide' : ''));
  row.appendChild(el('span', 'stat-label', label));
  row.appendChild(el('b', 'stat-value', value));
  return row;
}

function statGridFor(p, detail) {
  const stats = p.stats || {};
  const acc = p.accounts || { total: 0, available: 0 };
  const grid = el('div', 'stat-grid' + (detail ? ' detail-stat-grid' : ''));
  grid.appendChild(statRow('账号数', fmtNum(acc.total)));
  grid.appendChild(statRow('可用账号数', fmtNum(acc.available)));
  grid.appendChild(statRow('启用模型数', fmtNum((p.models || []).length)));
  grid.appendChild(statRow('总调用次数', fmtNum(stats.total_calls)));
  grid.appendChild(statRow('正在调用数', fmtNum(stats.active_calls)));
  grid.appendChild(statRow('成功率', successRate(stats)));
  grid.appendChild(statRow('Token 输入 / 输出', fmtNum(stats.prompt_tokens) + ' / ' + fmtNum(stats.completion_tokens), true));
  return grid;
}

function appendBadges(box, p) {
  const [tlabel, tkind] = toolsLabel(p.tools);
  box.appendChild(badge(tlabel, tkind));
  box.appendChild(badge(p.healthy ? '运行中' : '未运行', p.healthy ? 'ok' : 'err'));
  if (p.auth_type !== 'none') {
    box.appendChild(badge(p.auth_ready ? '凭证已配置' : '缺少凭证', p.auth_ready ? 'ok' : 'warn'));
  }
}

function pluginBadges(box, p) {
  box.innerHTML = '';
  appendBadges(box, p);
}

function renderPlugins() {
  const box = document.getElementById('plugins');
  box.innerHTML = '';
  const q = (state.pluginQuery || '').toLowerCase();
  const list = state.plugins.filter(p => {
    if (!q) return true;
    if ((p.name || '').toLowerCase().includes(q)) return true;
    return (p.models || []).some(m => String(m).toLowerCase().includes(q));
  });

  let native = 0, builtin = 0, none = 0;
  state.plugins.forEach(p => {
    if (p.tools === 'native') native++;
    else if (p.tools === 'builtin') builtin++;
    else none++;
  });
  let summary = '共 ' + state.plugins.length + ' 个插件 · 工具透传 ' + native + ' · 平台内置 ' + builtin + ' · 无工具 ' + none;
  if (q) summary += ' · 匹配 ' + list.length;
  document.getElementById('plugin-summary').textContent = summary;

  if (!list.length) {
    box.appendChild(el('div', 'muted', q ? '没有匹配的站点。' : '暂无插件。'));
    return;
  }

  const grid = el('div', 'grid site-grid');
  list.forEach(p => {
    const card = el('div', 'card plugin-card');
    card.tabIndex = 0;
    card.setAttribute('role', 'button');
    card.title = '点击查看详情';
    card.onclick = () => openDetail(p.name);
    card.onkeydown = function (e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        openDetail(p.name);
      }
    };

    const head = el('div', 'plugin-head');
    head.appendChild(el('span', 'plugin-name', p.name));
    appendBadges(head, p);
    card.appendChild(head);
    card.appendChild(statGridFor(p, false));
    card.appendChild(el('div', 'plugin-detail-hint', '查看详情 ›'));
    grid.appendChild(card);
  });
  box.appendChild(grid);
}

async function loadModels() {
  const data = await apiJSON('/api/models');
  if (!data) return;
  state.models = data.data || [];
  renderModels();
}

const MODEL_PAGE_SIZE = 5;

function renderModels() {
  const box = document.getElementById('models');
  box.innerHTML = '';

  const bySite = {};
  state.models.forEach(function (m) {
    (m.sources || []).forEach(function (s) {
      if (!bySite[s.plugin]) bySite[s.plugin] = { tools: s.tools, models: [] };
      bySite[s.plugin].models.push({ id: m.id, ready: s.ready });
    });
  });

  const order = {};
  state.plugins.forEach(function (p, i) { order[p.name] = i; });
  const names = Object.keys(bySite).sort(function (a, b) {
    const oa = order[a] === undefined ? 9999 : order[a];
    const ob = order[b] === undefined ? 9999 : order[b];
    return oa !== ob ? oa - ob : a.localeCompare(b);
  });

  const q = state.modelQuery || '';
  let matched = 0;
  const groups = [];
  names.forEach(function (name) {
    const site = bySite[name];
    const siteMatch = q && name.toLowerCase().indexOf(q) >= 0;
    const models = site.models
      .filter(function (m) { return !q || siteMatch || m.id.toLowerCase().indexOf(q) >= 0; })
      .sort(function (a, b) { return a.id.localeCompare(b.id); });
    if (!models.length) return;
    matched += models.length;
    groups.push({ name: name, tools: site.tools, models: models });
  });

  document.getElementById('model-summary').textContent =
    '共 ' + state.models.length + ' 个模型 · ' + names.length + ' 个站点' + (q ? ' · 匹配 ' + matched + ' 个来源' : '');
  const toggleBtn = document.getElementById('model-toggle-all');
  if (toggleBtn) toggleBtn.textContent = state.allCollapsed ? '全部展开' : '全部收起';

  if (!groups.length) {
    box.appendChild(el('div', 'muted', q ? '没有匹配的模型或站点。' : '暂无模型。'));
    renderPager('model-pager', 0, 1, MODEL_PAGE_SIZE, function () {});
    return;
  }

  const pages = Math.max(1, Math.ceil(groups.length / MODEL_PAGE_SIZE));
  if (state.modelPage > pages) state.modelPage = pages;
  const start = (state.modelPage - 1) * MODEL_PAGE_SIZE;

  groups.slice(start, start + MODEL_PAGE_SIZE).forEach(function (group) {
    const collapsed = !q && (state.collapsedSites[group.name] !== undefined
      ? state.collapsedSites[group.name]
      : state.allCollapsed);
    const section = el('div', 'site-group' + (collapsed ? ' collapsed' : ''));

    const head = el('div', 'site-group-head');
    head.appendChild(el('span', 'chevron', '▾'));
    head.appendChild(el('span', 'name', group.name));
    const [tlabel, tkind] = toolsLabel(group.tools || 'none');
    head.appendChild(badge(tlabel, tkind));
    head.appendChild(el('span', 'count', group.models.length + ' 个模型'));
    head.onclick = function () { toggleSite(group.name, collapsed); };
    section.appendChild(head);

    const body = el('div', 'site-group-body');
    const wrap = el('div', 'table-wrap');
    const table = el('table', 'data-table');
    const thead = el('thead');
    const headRow = el('tr');
    headRow.appendChild(el('th', '', '模型'));
    headRow.appendChild(el('th', '', '状态'));
    thead.appendChild(headRow);
    table.appendChild(thead);

    const tbody = el('tbody');
    group.models.forEach(function (m) {
      const tr = el('tr');
      tr.appendChild(el('td', 'mono', m.id));
      const statusTd = el('td');
      statusTd.appendChild(badge(m.ready ? '可用' : '未就绪', m.ready ? 'ok' : 'warn'));
      tr.appendChild(statusTd);
      tbody.appendChild(tr);
    });
    table.appendChild(tbody);
    wrap.appendChild(table);
    body.appendChild(wrap);
    section.appendChild(body);
    box.appendChild(section);
  });

  renderPager('model-pager', groups.length, state.modelPage, MODEL_PAGE_SIZE, function (p) {
    state.modelPage = p;
    renderModels();
  });
}

// toggleSite 收起或展开某个站点分组。
function toggleSite(name, collapsed) {
  state.collapsedSites[name] = !collapsed;
  renderModels();
}

function credLabel(field) {
  const labels = {
    api_key: 'API Key',
    bearer: 'Bearer Token',
    token: 'Token',
    proxy: 'IP 代理'
  };
  return labels[field] || field;
}

function updateDetailHeader(p) {
  document.getElementById('detail-title').textContent = p.name;
  const badges = document.getElementById('detail-badges');
  badges.innerHTML = '';
  appendBadges(badges, p);
  const statsBox = document.getElementById('detail-stats');
  if (statsBox) {
    statsBox.innerHTML = '';
    statsBox.appendChild(statGridFor(p, true));
  }
}

function renderDetailBody(p) {
  const body = document.getElementById('detail-body');
  body.innerHTML = '';

  const statsSection = el('div', 'detail-section');
  statsSection.appendChild(el('h3', '', '运行统计'));
  const statsBox = el('div');
  statsBox.id = 'detail-stats';
  statsBox.appendChild(statGridFor(p, true));
  statsSection.appendChild(statsBox);
  body.appendChild(statsSection);

  const modelSection = el('div', 'detail-section');
  const models = p.models || [];
  modelSection.appendChild(el('h3', '', '模型（' + models.length + '）'));
  const chips = el('div', 'detail-models');
  if (models.length) {
    models.forEach(m => chips.appendChild(el('span', 'model-chip', String(m))));
  } else {
    chips.appendChild(el('span', 'muted', '暂无模型。'));
  }
  modelSection.appendChild(chips);
  body.appendChild(modelSection);

  const credSection = el('div', 'detail-section');
  credSection.appendChild(el('h3', '', '凭证管理'));
  const credBox = el('div');
  credBox.id = 'detail-creds';
  credBox.appendChild(el('div', 'muted', '加载中…'));
  credSection.appendChild(credBox);
  body.appendChild(credSection);

  if (p.last_error) {
    const err = el('div', 'detail-note');
    err.style.color = 'var(--notion-red)';
    err.textContent = '最近错误：' + p.last_error;
    body.appendChild(err);
  }
}

async function openDetail(name) {
  const p = state.plugins.find(x => x.name === name);
  if (!p) return;
  state.detailName = name;
  updateDetailHeader(p);
  renderDetailBody(p);
  document.getElementById('detail-overlay').classList.remove('hidden');
  document.body.style.overflow = 'hidden';
  await loadDetailCreds(name);
}

function closeDetail() {
  state.detailName = '';
  document.getElementById('detail-overlay').classList.add('hidden');
  document.getElementById('detail-body').innerHTML = '';
  document.body.style.overflow = '';
}

async function loadDetailCreds(name) {
  const box = document.getElementById('detail-creds');
  if (!box) return;
  const data = await apiJSON('/api/credentials');
  if (!data || state.detailName !== name) return;
  const c = (data.credentials || []).find(x => x.plugin === name);
  box.innerHTML = '';
  if (!c) {
    box.appendChild(el('div', 'muted', '暂无凭证配置。'));
    return;
  }

  const anonymous = c.auth_type === 'none' || c.auth_type === '';
  box.appendChild(el('div', 'muted detail-note', anonymous
    ? '本站匿名可用。账号数按代理池中的代理数量统计，未配置代理池时按本机直连 1 个 IP 计算。'
    : '账号数按 key 数统计：每行一个 key，网关按调用轮换。字段值加密存储，写入后不再回显。'));

  const fields = (c.fields || []).filter(f => f !== 'proxy');
  fields.forEach(f => {
    const configured = (c.configured || []).includes(f);
    const count = (c.counts && c.counts[f]) || 0;
    const wrap = el('div', 'cred-field');
    const label = el('label');
    label.textContent = credLabel(f) + ' · ';
    if (count > 0) label.textContent += count + ' 个 key';
    else if (configured) label.textContent += '已配置';
    else label.textContent += '未配置';
    wrap.appendChild(label);

    const ta = document.createElement('textarea');
    ta.placeholder = '每行一个 key，可一次粘贴多个';
    wrap.appendChild(ta);

    const actions = el('div', 'cred-actions');
    if (configured) {
      const del = el('button', 'btn danger', '清除');
      del.onclick = () => deleteDetailCred(name, f);
      actions.appendChild(del);
    }
    const save = el('button', 'btn primary', '保存');
    save.onclick = () => saveDetailCred(name, f, ta);
    actions.appendChild(save);
    wrap.appendChild(actions);
    box.appendChild(wrap);
  });
}

async function refreshAfterCredChange(name) {
  await loadPlugins();
  await loadModels();
  if (state.detailName === name) await loadDetailCreds(name);
}

async function saveDetailCred(plugin, field, textarea) {
  const value = textarea.value;
  if (!value.trim()) return;
  const res = await fetch('/api/credentials', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ plugin, field, value })
  });
  if (res.status === 401) { showLogin(); return; }
  if (!res.ok) {
    let msg = '保存失败';
    try {
      const data = await res.json();
      if (data && data.error && data.error.message) msg = data.error.message;
    } catch (e) {}
    await uiAlert(msg, '保存失败');
    return;
  }
  textarea.value = '';
  await refreshAfterCredChange(plugin);
  toast('凭证已保存');
}

async function deleteDetailCred(plugin, field) {
  const res = await fetch('/api/credentials?plugin=' + encodeURIComponent(plugin) + '&field=' + encodeURIComponent(field), { method: 'DELETE' });
  if (res.status === 401) { showLogin(); return; }
  await refreshAfterCredChange(plugin);
}

document.getElementById('detail-overlay').addEventListener('click', function (e) {
  if (e.target === this) closeDetail();
});

document.getElementById('dialog-overlay').addEventListener('click', function (e) {
  if (e.target === this) closeDialog(false);
});

document.getElementById('key-modal').addEventListener('click', function (e) {
  if (e.target === this) closeKeyModal();
});

document.getElementById('proxy-modal').addEventListener('click', function (e) {
  if (e.target === this) closeProxyModal();
});

window.addEventListener('keydown', function (e) {
  if (e.key !== 'Escape') return;
  if (!document.getElementById('dialog-overlay').classList.contains('hidden')) {
    closeDialog(false);
    return;
  }
  if (!document.getElementById('key-modal').classList.contains('hidden')) {
    closeKeyModal();
    return;
  }
  if (!document.getElementById('proxy-modal').classList.contains('hidden')) {
    closeProxyModal();
    return;
  }
  if (state.detailName) closeDetail();
});

document.getElementById('plugin-search').addEventListener('input', function () {
  state.pluginQuery = this.value.trim().toLowerCase();
  renderPlugins();
});

document.getElementById('model-search').addEventListener('input', function () {
  state.modelQuery = this.value.trim().toLowerCase();
  state.modelPage = 1;
  renderModels();
});

function applyPluginSearch() {
  state.pluginQuery = document.getElementById('plugin-search').value.trim().toLowerCase();
  renderPlugins();
}

function clearPluginSearch() {
  document.getElementById('plugin-search').value = '';
  state.pluginQuery = '';
  renderPlugins();
}

function applyModelSearch() {
  state.modelQuery = document.getElementById('model-search').value.trim().toLowerCase();
  state.modelPage = 1;
  renderModels();
}

function clearModelSearch() {
  document.getElementById('model-search').value = '';
  state.modelQuery = '';
  state.modelPage = 1;
  renderModels();
}

function toggleAllSites() {
  state.allCollapsed = !state.allCollapsed;
  state.collapsedSites = {};
  renderModels();
}

// renderPager 渲染分页控件；total 不超过一页时隐藏。
function renderPager(boxId, total, page, perPage, onGo) {
  const box = document.getElementById(boxId);
  box.innerHTML = '';
  const pages = Math.max(1, Math.ceil(total / perPage));
  if (total <= perPage) {
    box.classList.add('hidden');
    return;
  }
  box.classList.remove('hidden');
  box.appendChild(el('span', '', '共 ' + total + ' 条'));
  const prev = el('button', 'btn', '上一页');
  prev.disabled = page <= 1;
  prev.onclick = function () { if (page > 1) onGo(page - 1); };
  box.appendChild(prev);
  box.appendChild(el('span', 'current', page + ' / ' + pages));
  const next = el('button', 'btn', '下一页');
  next.disabled = page >= pages;
  next.onclick = function () { if (page < pages) onGo(page + 1); };
  box.appendChild(next);
}

// readError 提取接口错误信息。
async function readError(res, fallback) {
  try {
    const data = await res.json();
    if (data && data.error && data.error.message) return data.error.message;
  } catch (e) {}
  return fallback;
}

const PROXY_PAGE_SIZE = 10;
const KEY_PAGE_SIZE = 10;

async function loadProxyPool() {
  const data = await apiJSON('/api/proxy');
  if (!data) return;
  state.proxy = data;
  renderProxyPool();
}

function renderProxyPool() {
  const box = document.getElementById('proxy-list');
  box.innerHTML = '';
  const proxies = state.proxy.proxies || [];
  document.getElementById('proxy-summary').textContent = proxies.length
    ? '共 ' + proxies.length + ' 个代理'
    : '代理池为空';
  const pages = Math.max(1, Math.ceil(proxies.length / PROXY_PAGE_SIZE));
  if (state.proxyPage > pages) state.proxyPage = pages;
  if (!proxies.length) {
    box.appendChild(el('div', 'table-empty', '当前没有代理，所有站点使用本机直连。'));
    renderPager('proxy-pager', 0, 1, PROXY_PAGE_SIZE, function () {});
    return;
  }

  const start = (state.proxyPage - 1) * PROXY_PAGE_SIZE;
  const wrap = el('div', 'table-wrap');
  const table = el('table', 'data-table');
  const thead = el('thead');
  const headRow = el('tr');
  headRow.appendChild(el('th', '', '#'));
  headRow.appendChild(el('th', '', '代理地址'));
  headRow.appendChild(el('th', '', '操作'));
  thead.appendChild(headRow);
  table.appendChild(thead);

  const tbody = el('tbody');
  proxies.slice(start, start + PROXY_PAGE_SIZE).forEach(function (p, i) {
    const idx = start + i;
    const tr = el('tr');
    tr.appendChild(el('td', 'muted', String(idx + 1)));
    tr.appendChild(el('td', 'mono', p));

    const actionsTd = el('td');
    const actions = el('div', 'table-actions');
    const del = el('button', 'btn danger', '删除');
    del.onclick = function () { deleteProxy(idx, p); };
    actions.appendChild(del);
    actionsTd.appendChild(actions);
    tr.appendChild(actionsTd);
    tbody.appendChild(tr);
  });
  table.appendChild(tbody);
  wrap.appendChild(table);
  box.appendChild(wrap);
  renderPager('proxy-pager', proxies.length, state.proxyPage, PROXY_PAGE_SIZE, function (p) {
    state.proxyPage = p;
    renderProxyPool();
  });
}

function openProxyModal() {
  document.getElementById('proxy-modal').classList.remove('hidden');
  setTimeout(function () { document.getElementById('proxy-modal-input').focus(); }, 30);
}

function closeProxyModal() {
  document.getElementById('proxy-modal').classList.add('hidden');
  document.getElementById('proxy-modal-input').value = '';
}

async function submitProxies() {
  const input = document.getElementById('proxy-modal-input');
  if (!input.value.trim()) { await uiAlert('请先输入至少一个代理'); return; }
  const res = await fetch('/api/proxy', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ value: input.value, append: true })
  });
  if (res.status === 401) { showLogin(); return; }
  if (!res.ok) { await uiAlert(await readError(res, '添加失败'), '添加失败'); return; }
  state.proxy = await res.json();
  closeProxyModal();
  state.proxyPage = Math.max(1, Math.ceil((state.proxy.proxies || []).length / PROXY_PAGE_SIZE));
  renderProxyPool();
  await loadPlugins();
  toast('代理已添加');
}

async function deleteProxy(index, value) {
  if (!await uiConfirm('删除代理 ' + value + '？', { title: '删除代理', danger: true, okText: '删除' })) return;
  const res = await fetch('/api/proxy?index=' + index, { method: 'DELETE' });
  if (res.status === 401) { showLogin(); return; }
  if (!res.ok) { await uiAlert(await readError(res, '删除失败'), '删除失败'); return; }
  state.proxy = await res.json();
  renderProxyPool();
  await loadPlugins();
  toast('代理已删除');
}

async function clearProxyPool() {
  if (!await uiConfirm('清空代理池？所有站点将改为本机直连。', { title: '清空代理池', danger: true, okText: '清空' })) return;
  const res = await fetch('/api/proxy', { method: 'DELETE' });
  if (res.status === 401) { showLogin(); return; }
  if (!res.ok) { await uiAlert(await readError(res, '清空失败'), '清空失败'); return; }
  state.proxy = await res.json();
  state.proxyPage = 1;
  renderProxyPool();
  await loadPlugins();
  toast('代理池已清空');
}

async function loadSettings() {
  const data = await apiJSON('/api/settings');
  if (data) applySettings(data);
}

function applySettings(data) {
  if (!data) return;
  state.settings = data;
  renderKeyTable(data.keys || []);
  document.getElementById('settings-password-state').textContent = data.password_set
    ? '已设置登录密码'
    : '未设置密码，控制台免登录';
}

function fmtTime(value) {
  if (!value) return '—';
  const d = new Date(value);
  return isNaN(d.getTime()) ? value : d.toLocaleString();
}

// renderKeyTable 渲染网关 API Key 列表：状态、调用次数、最近调用与操作。
function renderKeyTable(keys) {
  const box = document.getElementById('settings-keys-table');
  box.innerHTML = '';
  const pages = Math.max(1, Math.ceil(keys.length / KEY_PAGE_SIZE));
  if (state.keyPage > pages) state.keyPage = pages;
  if (!keys.length) {
    box.appendChild(el('div', 'table-empty', '未设置 Key，/v1 接口不鉴权。'));
    renderPager('keys-pager', 0, 1, KEY_PAGE_SIZE, function () {});
    return;
  }

  const start = (state.keyPage - 1) * KEY_PAGE_SIZE;
  const wrap = el('div', 'table-wrap');
  const table = el('table', 'data-table');
  const thead = el('thead');
  const headRow = el('tr');
  ['Key', '状态', '调用次数', '最近调用', '操作'].forEach(function (label, i) {
    headRow.appendChild(el('th', i === 2 ? 'num' : '', label));
  });
  thead.appendChild(headRow);
  table.appendChild(thead);

  const tbody = el('tbody');
  keys.slice(start, start + KEY_PAGE_SIZE).forEach(function (k) {
    const tr = el('tr');
    tr.appendChild(el('td', 'mono', k.masked));

    const statusTd = el('td');
    statusTd.appendChild(badge(k.enabled ? '可用' : '已停用', k.enabled ? 'ok' : 'err'));
    tr.appendChild(statusTd);

    tr.appendChild(el('td', 'num', fmtNum(k.calls)));
    tr.appendChild(el('td', 'muted', fmtTime(k.last_used)));

    const actionsTd = el('td');
    const actions = el('div', 'table-actions');
    const toggle = el('button', 'btn', k.enabled ? '停用' : '启用');
    toggle.onclick = function () { toggleApiKey(k.id, !k.enabled); };
    actions.appendChild(toggle);
    const del = el('button', 'btn danger', '删除');
    del.onclick = function () { deleteApiKey(k.id, k.masked); };
    actions.appendChild(del);
    actionsTd.appendChild(actions);
    tr.appendChild(actionsTd);

    tbody.appendChild(tr);
  });
  table.appendChild(tbody);
  wrap.appendChild(table);
  box.appendChild(wrap);
  renderPager('keys-pager', keys.length, state.keyPage, KEY_PAGE_SIZE, function (p) {
    state.keyPage = p;
    renderKeyTable(keys);
  });
}

function openKeyModal() {
  document.getElementById('key-modal').classList.remove('hidden');
  setTimeout(function () { document.getElementById('key-modal-input').focus(); }, 30);
}

function closeKeyModal() {
  document.getElementById('key-modal').classList.add('hidden');
  document.getElementById('key-modal-input').value = '';
}

async function submitKeys() {
  const input = document.getElementById('key-modal-input');
  const lines = input.value.split('\n').map(s => s.trim()).filter(Boolean);
  if (!lines.length) { await uiAlert('请先输入至少一个 Key'); return; }
  const res = await fetch('/api/settings', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ add_keys: lines })
  });
  if (res.status === 401) { showLogin(); return; }
  if (!res.ok) { await uiAlert(await readError(res, '保存失败'), '保存失败'); return; }
  const data = await res.json();
  state.keyPage = Math.max(1, Math.ceil((data.keys || []).length / KEY_PAGE_SIZE));
  closeKeyModal();
  applySettings(data);
  toast('API Key 已保存');
}

async function toggleApiKey(id, enabled) {
  const res = await fetch('/api/settings', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ toggle_key: id, key_enabled: enabled })
  });
  if (res.status === 401) { showLogin(); return; }
  if (!res.ok) { await uiAlert(await readError(res, '操作失败'), '操作失败'); return; }
  applySettings(await res.json());
  toast(enabled ? 'Key 已启用' : 'Key 已停用');
}

async function deleteApiKey(id, masked) {
  if (!await uiConfirm('删除 Key ' + masked + '？使用该 Key 的请求会立即失败。', { title: '删除 Key', danger: true, okText: '删除' })) return;
  const res = await fetch('/api/settings', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ delete_key: id })
  });
  if (res.status === 401) { showLogin(); return; }
  if (!res.ok) { await uiAlert(await readError(res, '删除失败'), '删除失败'); return; }
  applySettings(await res.json());
  toast('Key 已删除');
}

async function savePassword() {
  const p1 = document.getElementById('settings-password').value;
  const p2 = document.getElementById('settings-password2').value;
  if (!p1) { await uiAlert('请输入新密码'); return; }
  if (p1 !== p2) { await uiAlert('两次输入的密码不一致'); return; }
  const res = await fetch('/api/settings', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: p1 })
  });
  if (res.status === 401) { showLogin(); return; }
  if (!res.ok) { await uiAlert(await readError(res, '保存失败'), '保存失败'); return; }
  applySettings(await res.json());
  document.getElementById('settings-password').value = '';
  document.getElementById('settings-password2').value = '';
  toast('登录密码已保存');
}

async function clearPassword() {
  if (!await uiConfirm('清除登录密码？之后打开控制台将不再需要登录。', { title: '清除密码', danger: true, okText: '清除' })) return;
  const res = await fetch('/api/settings', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ clear_password: true })
  });
  if (res.status === 401) { showLogin(); return; }
  if (!res.ok) { await uiAlert(await readError(res, '清除失败'), '清除失败'); return; }
  applySettings(await res.json());
  toast('登录密码已清除');
}

function restoreScroll() {
  let y = 0;
  try { y = parseInt(localStorage.getItem(SCROLL_STORAGE_KEY) || '0', 10) || 0; } catch (e) {}
  if (y > 0) window.scrollTo(0, y);
}

window.addEventListener('hashchange', function () {
  const tab = (location.hash || '').replace('#', '');
  if (TAB_NAMES.indexOf(tab) >= 0) activateTab(tab, true);
});

async function initData() {
  await loadPlugins();
  await loadModels();
  await loadProxyPool();
  await loadSettings();
  restoreScroll();
}

async function boot() {
  let session = null;
  try {
    const res = await fetch('/api/session');
    if (res.ok) session = await res.json();
  } catch (e) {}
  if (session && session.password_set && !session.logged_in) {
    showLogin();
    return;
  }
  hideLogin();
  await initData();
}

document.getElementById('login-password').addEventListener('keydown', function (e) {
  if (e.key === 'Enter') doLogin();
});

boot();
setInterval(function () {
  if (!state.locked) loadPlugins();
}, 10000);
</script>
</body>
</html>
`
