const state = { token: sessionStorage.getItem("meridian_admin_token") || "", access: null, merchants: [], merchantId: "", payments: [], deliveries: [], audits: [], applications: [], staticCodes: [], fees: [], refunds: [], alipay: null, view: "overview" };
const $ = (selector) => document.querySelector(selector);
const escapeHTML = (value) => String(value ?? "").replace(/[&<>'"]/g, (char) => ({"&":"&amp;","<":"&lt;",">":"&gt;","'":"&#39;",'"':"&quot;"})[char]);
const alipayOfficialLinks = {
  sandboxConsole: "https://open.alipay.com/develop/sandbox/app",
  sandboxGuide: "https://opendocs.alipay.com/common/02kkv7",
  applicationConsole: "https://open.alipay.com/develop/manage",
  keyTool: "https://opendocs.alipay.com/common/02kipk",
  keySetup: "https://opendocs.alipay.com/common/02kdnc",
  productCenter: "https://b.alipay.com/page/product-mall/all-product",
};
const money = (minor, currency = "CNY") => new Intl.NumberFormat("zh-CN", {style:"currency", currency}).format(Number(minor || 0) / 100);
const timeText = (value) => value ? new Intl.DateTimeFormat("zh-CN", {dateStyle:"short", timeStyle:"medium"}).format(new Date(value)) : "-";
const statusLabels = {active:"正常",disabled:"已停用",created:"待初始化",pending:"待支付",paid:"已支付",failed:"失败",closed:"已关闭",refunding:"退款处理中",partially_refunded:"部分退款",refunded:"已退款",delivered:"已送达",skipped:"已跳过",exhausted:"重试耗尽",succeeded:"成功"};
const methodLabels = {static_alipay:"支付宝静态码",alipay_qr:"支付宝动态码",alipay_page:"支付宝电脑网站",alipay_wap:"支付宝手机网站"};
const eventLabels = {"payment.paid":"支付成功","payment.closed":"支付关闭","refund.updated":"退款更新"};
const auditLabels = {"channel_account.updated":"支付宝账户已更新",manual_confirmation:"人工确认到账","refund.created":"已创建退款","payment.abnormal":"已标记异常","merchant.admin_token_rotated":"商户登录令牌已轮换","application.updated":"应用已更新","application.credentials_rotated":"应用凭证已轮换","delivery.retried":"Webhook 已重新排队"};
const actorLabels = {system_admin:"系统管理员",admin:"系统管理员","alipay.notify":"支付宝通知","alipay.query":"支付宝查询"};
const feeLabels = {processing:"处理费"};
const labelFor = (labels, value) => labels[value] || String(value || "-");

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  headers.set("Authorization", `Bearer ${state.token}`);
  if (options.body && !(options.body instanceof FormData)) headers.set("Content-Type", "application/json");
  const response = await fetch(path, {...options, headers});
  const contentType = response.headers.get("content-type") || "";
  const body = contentType.includes("application/json") ? await response.json() : await response.text();
  if (!response.ok) throw new Error(body?.error?.message || body || `HTTP ${response.status}`);
  return body;
}

function toast(message, error = false) {
  const node = $("#toast"); node.textContent = message; node.classList.toggle("error", error); node.classList.remove("hidden");
  clearTimeout(toast.timer); toast.timer = setTimeout(() => node.classList.add("hidden"), 4200);
}

function showApp() { $("#login").classList.add("hidden"); $("#app").classList.remove("hidden"); }
function showLogin() { $("#app").classList.add("hidden"); $("#login").classList.remove("hidden"); }

async function loadSession() {
  state.access = await api("/admin/api/session");
  const system = state.access.role === "system_admin";
  $("#access-scope").textContent = system ? "系统管理员" : "商户管理员";
  $("#add-merchant").classList.toggle("hidden", !system);
  $("#rotate-merchant-token").classList.toggle("hidden", !system);
}

async function checkHealth() {
  const node = $("#service-status");
  try {
    const response = await fetch("/healthz", {cache:"no-store"});
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    node.classList.remove("error"); node.title = "服务正常";
  } catch (_) {
    node.classList.add("error"); node.title = "服务不可用";
  }
}

async function loadMerchants() {
  const data = await api("/admin/api/merchants"); state.merchants = data.items || [];
  if (!state.merchantId || !state.merchants.some((item) => item.id === state.merchantId)) state.merchantId = state.merchants[0]?.id || "";
  const select = $("#merchant-select"); select.innerHTML = state.merchants.length ? state.merchants.map((item) => `<option value="${escapeHTML(item.id)}" ${item.id === state.merchantId ? "selected" : ""}>${escapeHTML(item.name)}</option>`).join("") : '<option value="">暂无商户</option>';
  select.disabled = !state.merchants.length || state.access?.role === "merchant_admin";
}

async function loadMerchantData() {
  if (!state.merchantId) { state.payments=[];state.deliveries=[];state.audits=[];state.applications=[];state.staticCodes=[];state.fees=[];state.refunds=[];state.alipay=null;render();return; }
  const base = `/admin/api/merchants/${encodeURIComponent(state.merchantId)}`;
  const [payments, deliveries, audits, applications, staticCodes, fees, refunds, alipay] = await Promise.all([
    api(`${base}/payments`),
    api(`${base}/deliveries`), api(`${base}/audits`), api(`${base}/applications`), api(`${base}/static-codes`), api(`${base}/fees`), api(`${base}/refunds`), api(`${base}/alipay`).catch(() => null)
  ]);
  state.payments = payments.items || []; state.deliveries = deliveries.items || []; state.audits = audits.items || []; state.applications = applications.items || []; state.staticCodes = staticCodes.items || []; state.fees = fees.items || []; state.refunds = refunds.items || []; state.alipay = alipay;
  render();
}

async function refresh() { await checkHealth(); try { await loadSession(); await loadMerchants(); await loadMerchantData(); return true; } catch (error) { if (/token|Unauthorized|invalid_admin/i.test(error.message)) { sessionStorage.removeItem("meridian_admin_token"); state.token="";state.access=null;showLogin(); } toast(error.message, true); return false; } }

function render() {
  const paid = state.payments.filter((item) => ["paid","partially_refunded","refunded"].includes(item.status)).length;
  const pending = state.payments.filter((item) => ["created","pending"].includes(item.status)).length;
  const failed = state.deliveries.filter((item) => ["failed","exhausted","skipped"].includes(item.status)).length;
  $("#metric-payments").textContent = state.payments.length; $("#metric-paid").textContent = paid; $("#metric-pending").textContent = pending; $("#metric-failed").textContent = failed;
  $("#overview-payments").innerHTML = paymentTable(state.payments.slice(0, 8), false);
  $("#payments-table").innerHTML = paymentTable(filteredPayments(), true);
  $("#fee-table").innerHTML = feeTable(); $("#refund-table").innerHTML = refundTable();
  $("#delivery-table").innerHTML = deliveryTable(); $("#audit-table").innerHTML = auditTable(); $("#application-table").innerHTML = applicationTable(); $("#static-code-table").innerHTML = staticCodeTable();
  $("#alipay-state").innerHTML = state.alipay ? `<strong>${escapeHTML(state.alipay.app_id)}</strong><br><span>${state.alipay.environment === "production" ? "正式环境" : "沙箱环境"}</span><br><span class="muted mono">${escapeHTML(state.alipay.notify_url)}</span><br>${status(state.alipay.status)}` : '<span class="muted">未配置</span>';
  $("#reconciliation").classList.toggle("disabled", !state.merchantId);
}

function status(value) { return `<span class="pill ${escapeHTML(value)}">${escapeHTML(labelFor(statusLabels,value))}</span>`; }
function filteredPayments() {
  const filter = $("#status-filter")?.value || "";
  if (!filter) return state.payments;
  const groups = {open:["created","pending"],paid:["paid"],closed:["closed"],failed:["failed"],refund:["refunding","partially_refunded","refunded"]};
  return state.payments.filter((item)=>groups[filter]?.includes(item.status));
}
function paymentTable(items, actions) {
  if (!items.length) return '<div class="empty">暂无订单</div>';
  return `<table><thead><tr><th>订单</th><th>方式</th><th>金额</th><th>状态</th><th>创建时间</th>${actions?'<th>操作</th>':''}</tr></thead><tbody>${items.map((item)=>`<tr><td><strong>${escapeHTML(item.subject)}</strong><br><span class="muted mono">${escapeHTML(item.merchant_order_id)}</span></td><td>${escapeHTML(labelFor(methodLabels,item.method))}</td><td>${money(item.amount_minor,item.currency)}</td><td>${status(item.status)}${item.last_error?'<br><span class="field-error">需处理</span>':''}</td><td>${timeText(item.created_at)}</td>${actions?`<td class="actions"><div class="actions-row">${paymentActions(item)}</div></td>`:''}</tr>`).join("")}</tbody></table>`;
}
function feeTable(){if(!state.fees.length)return'<div class="empty">暂无费用记录</div>';return`<table><thead><tr><th>订单 ID</th><th>类型</th><th>金额</th><th>时间</th></tr></thead><tbody>${state.fees.map((item)=>`<tr><td class="mono">${escapeHTML(item.payment_id)}</td><td>${escapeHTML(labelFor(feeLabels,item.kind))}</td><td>${money(item.amount_minor,item.currency)}</td><td>${timeText(item.created_at)}</td></tr>`).join('')}</tbody></table>`;}
function refundTable(){if(!state.refunds.length)return'<div class="empty">暂无退款记录</div>';return`<table><thead><tr><th>退款号</th><th>订单 ID</th><th>金额</th><th>状态</th><th>时间</th></tr></thead><tbody>${state.refunds.map((item)=>`<tr><td class="mono">${escapeHTML(item.merchant_refund_id)}</td><td class="mono">${escapeHTML(item.payment_id)}</td><td>${money(item.amount_minor,'CNY')}</td><td>${status(item.status)}</td><td>${timeText(item.created_at)}</td></tr>`).join('')}</tbody></table>`;}
function paymentActions(item) {
  const id = escapeHTML(item.id); const buttons=[`<button data-action="details" data-id="${id}">详情</button>`];
  if (["created","pending"].includes(item.status) && item.method === "static_alipay") buttons.push(`<button data-action="confirm" data-id="${id}">确认到账</button>`);
  if (["created","pending"].includes(item.status) && item.method !== "static_alipay") buttons.push(`<button data-action="query" data-id="${id}">查询</button>`);
  if (["created","pending"].includes(item.status)) buttons.push(`<button data-action="close" data-id="${id}">关闭</button>`,`<button data-action="abnormal" data-id="${id}">异常</button>`);
  if (["paid","partially_refunded"].includes(item.status) && item.method !== "static_alipay") buttons.push(`<button data-action="refund" data-id="${id}" data-remaining="${item.amount_minor-item.refunded_minor}">退款</button>`);
  return buttons.join("");
}
function deliveryTable() { if(!state.deliveries.length)return'<div class="empty">暂无投递记录</div>';return`<table><thead><tr><th>事件</th><th>聚合 ID</th><th>状态</th><th>尝试</th><th>响应</th><th>时间</th><th>操作</th></tr></thead><tbody>${state.deliveries.map((item)=>`<tr><td>${escapeHTML(labelFor(eventLabels,item.event_type))}</td><td class="mono">${escapeHTML(item.aggregate_id)}</td><td>${status(item.status)}</td><td>${item.attempts}</td><td>${item.last_status_code||'-'}${item.last_error?`<br><span class="muted">${escapeHTML(item.last_error==='webhook_url is empty'?'未配置 Webhook 地址':item.last_error)}</span>`:''}</td><td>${timeText(item.created_at)}</td><td>${["skipped","failed","exhausted"].includes(item.status)?`<button data-delivery-retry="${escapeHTML(item.id)}">重新投递</button>`:'-'}</td></tr>`).join("")}</tbody></table>`; }
function readableAuditDetails(value){try{const labels={app_id:'App ID',gateway_url:'支付宝网关',amount_minor:'订单金额',fee_minor:'处理费',note:'备注',status:'状态',webhook_configured:'Webhook'};const data=JSON.parse(value||'{}');return Object.entries(data).map(([key,item])=>{let shown=item;if(key.endsWith('_minor'))shown=money(item);else if(key==='status')shown=labelFor(statusLabels,item);else if(key==='webhook_configured')shown=item?'已配置':'未配置';return`${labels[key]||key}: ${shown}`}).join(' · ')||'-'}catch(_){return value||'-'}}
function auditTable(){if(!state.audits.length)return'<div class="empty">暂无审计记录</div>';return`<table><thead><tr><th>动作</th><th>操作者</th><th>资源</th><th>详情</th><th>时间</th></tr></thead><tbody>${state.audits.map((item)=>`<tr><td>${escapeHTML(labelFor(auditLabels,item.action))}</td><td>${escapeHTML(item.actor.startsWith('merchant_admin:')?'商户管理员':labelFor(actorLabels,item.actor))}</td><td class="mono">${escapeHTML(item.resource_id)}</td><td>${escapeHTML(readableAuditDetails(item.details))}</td><td>${timeText(item.created_at)}</td></tr>`).join("")}</tbody></table>`;}
function applicationTable(){if(!state.applications.length)return'<div class="empty">暂无应用</div>';return`<table><thead><tr><th>名称</th><th>应用 ID</th><th>Webhook</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead><tbody>${state.applications.map((item)=>`<tr><td>${escapeHTML(item.name)}</td><td class="mono">${escapeHTML(item.id)}</td><td class="mono">${escapeHTML(item.webhook_url||'未配置')}</td><td>${status(item.status)}</td><td>${timeText(item.created_at)}</td><td><div class="actions-row"><button data-app-action="edit" data-id="${escapeHTML(item.id)}">编辑</button><button data-app-action="toggle" data-id="${escapeHTML(item.id)}">${item.status==='active'?'停用':'启用'}</button><button data-app-action="rotate" data-id="${escapeHTML(item.id)}">轮换凭证</button></div></td></tr>`).join("")}</tbody></table>`;}
function staticCodeTable(){if(!state.staticCodes.length)return'<div class="empty">暂无静态收款码</div>';return`<table><thead><tr><th>名称</th><th>门店</th><th>ID</th><th>状态</th><th>操作</th></tr></thead><tbody>${state.staticCodes.map((item)=>`<tr><td>${escapeHTML(item.name)}</td><td>${escapeHTML(item.store_name||'-')}</td><td class="mono">${escapeHTML(item.id)}</td><td>${status(item.enabled?'active':'disabled')}</td><td><button data-static-toggle="${escapeHTML(item.id)}" data-enabled="${item.enabled}">${item.enabled?'停用':'启用'}</button></td></tr>`).join("")}</tbody></table>`;}

function setView(view) {
  state.view=view; document.querySelectorAll(".nav button").forEach((node)=>node.classList.toggle("active",node.dataset.view===view)); document.querySelectorAll(".view").forEach((node)=>node.classList.add("hidden")); $(`#view-${view}`).classList.remove("hidden");
  const titles={overview:["运营概览","实时订单与投递状态"],payments:["支付订单","人工确认、渠道查询与退款"],events:["事件与审计","Webhook 投递和操作记录"],settings:["商户配置","应用、收款码和支付宝账户"]}; $("#view-title").textContent=titles[view][0];$("#view-subtitle").textContent=titles[view][1];
  loadMerchantData().catch((error)=>toast(error.message,true));
}

function openForm(title, fields, submitText, onSubmit, options = {}) {
  const dialog=$("#form-dialog"); dialog.classList.toggle("dialog-wide", options.wide === true);
  $("#dialog-title").textContent=title; const form=$("#dialog-form"); form.innerHTML=fields+`<div class="form-actions"><button data-close type="button">取消</button><button class="primary" type="submit">${escapeHTML(submitText)}</button></div>`;
  form.onsubmit=async(event)=>{event.preventDefault();const button=form.querySelector('button[type="submit"]');button.disabled=true;try{await onSubmit(new FormData(form));$("#form-dialog").close();await refresh()}catch(error){toast(error.message,true)}finally{button.disabled=false}};
  form.querySelectorAll("[data-close]").forEach((node)=>node.addEventListener("click",()=>dialog.close())); dialog.showModal();
}

function showSecret(title, content, html = false) {
  $("#secret-title").textContent = title;
  if (html) $("#secret-content").innerHTML = content;
  else $("#secret-content").textContent = content;
  $("#secret-dialog").showModal();
}

function officialLink(href, label) {
  return `<a href="${href}" target="_blank" rel="noopener noreferrer">${label}</a>`;
}

function alipayEnvironmentGuide(environment) {
  const sandbox = environment === "sandbox";
  const firstLink = sandbox
    ? officialLink(alipayOfficialLinks.sandboxConsole, "打开沙箱控制台")
    : officialLink(alipayOfficialLinks.applicationConsole, "打开应用控制台");
  const firstText = sandbox
    ? "登录后直接复制沙箱 App ID，并在“沙箱账号”中取得测试买家账号。"
    : "创建或打开网页/移动应用，复制应用详情中的 App ID，并确保应用已审核上线。";
  const fourthText = sandbox
    ? "沙箱无需签约正式支付产品，不会产生真实资金。使用沙箱账号完成付款。"
    : "按实际支付方式开通产品：动态码使用“当面付”，电脑和手机跳转分别开通对应网站支付。";
  const fourthLink = sandbox
    ? officialLink(alipayOfficialLinks.sandboxGuide, "查看沙箱快速接入")
    : officialLink(alipayOfficialLinks.productCenter, "打开支付宝产品中心");
  return `
    <div class="guide-summary ${sandbox ? "sandbox" : "production"}">
      <strong>${sandbox ? "建议先用沙箱完成验收" : "正式环境会发生真实资金交易"}</strong>
      <span>${sandbox ? "沙箱与正式环境的 App ID、密钥和账号不能混用。" : "请先在支付宝完成应用上线、密钥配置和产品签约。"}</span>
    </div>
    <ol class="setup-steps">
      <li><span class="step-number">1</span><div><strong>取得 App ID</strong><p>${firstText}</p><div class="guide-links">${firstLink}${sandbox ? officialLink(alipayOfficialLinks.sandboxGuide, "阅读官方沙箱指南") : ""}</div></div></li>
      <li><span class="step-number">2</span><div><strong>生成 RSA2 密钥</strong><p>在官方密钥工具中选择“密钥”和“RSA2”，生成应用公钥与应用私钥。应用私钥只粘贴到右侧。</p><div class="guide-links">${officialLink(alipayOfficialLinks.keyTool, "下载官方密钥工具")}</div></div></li>
      <li><span class="step-number">3</span><div><strong>配置接口加签</strong><p>把“应用公钥”提交到支付宝的接口加签设置，保存后复制支付宝生成的“支付宝公钥”。</p><div class="guide-links">${sandbox ? officialLink(alipayOfficialLinks.sandboxConsole, "返回沙箱控制台") : officialLink(alipayOfficialLinks.applicationConsole, "返回应用控制台")}${officialLink(alipayOfficialLinks.keySetup, "查看官方配置说明")}</div></div></li>
      <li><span class="step-number">4</span><div><strong>${sandbox ? "准备沙箱测试" : "开通支付产品"}</strong><p>${fourthText}</p><div class="guide-links">${fourthLink}</div></div></li>
    </ol>`;
}

function isLocalHostname(hostname) {
  return hostname === "localhost" || hostname === "::1" || hostname.endsWith(".local") || /^127\./.test(hostname) || /^10\./.test(hostname) || /^192\.168\./.test(hostname) || /^172\.(1[6-9]|2\d|3[01])\./.test(hostname);
}

function alipayCallbackGuide() {
  const baseURL = String(document.body.dataset.baseUrl || "").trim().replace(/\/+$/, "");
  const notifyURL = state.alipay?.notify_url || (baseURL ? `${baseURL}/v1/webhooks/alipay` : "");
  let ready = false;
  try {
    const parsed = new URL(notifyURL);
    ready = parsed.protocol === "https:" && !isLocalHostname(parsed.hostname);
  } catch (_) {}
  if (!notifyURL) {
    return '<div class="callback-check warning"><strong>通知地址尚未配置</strong><span>部署时设置 MERIDIAN_BASE_URL，Meridian 会自动生成支付宝通知地址。</span></div>';
  }
  return `<div class="callback-check ${ready ? "ready" : "warning"}"><strong>${ready ? "通知地址已按公网 HTTPS 配置" : "真实支付前还需配置公网 HTTPS 地址"}</strong><span class="mono">${escapeHTML(notifyURL)}</span><span>${ready ? "请确认该地址可从公网访问；无需在此手工修改。" : "当前是本机、内网或非 HTTPS 地址。可先保存配置，但支付宝无法可靠投递异步通知；请在部署环境修改 MERIDIAN_BASE_URL。"}</span></div>`;
}

function normalizePEM(value, label, fieldName) {
  const trimmed = String(value || "").trim();
  if (!trimmed) throw new Error(`请填写${fieldName}`);
  if (/-----BEGIN [^-]+-----/.test(trimmed)) return trimmed;
  const compact = trimmed.replace(/\s+/g, "");
  if (!/^[A-Za-z0-9+/]+={0,2}$/.test(compact)) throw new Error(`${fieldName}包含无法识别的字符，请从支付宝官方密钥工具重新复制`);
  const lines = compact.match(/.{1,64}/g) || [];
  return `-----BEGIN ${label}-----\n${lines.join("\n")}\n-----END ${label}-----`;
}

function readableAlipayError(error) {
  const message = String(error?.message || error || "保存失败");
  if (/MERIDIAN_BASE_URL/i.test(message)) return "保存失败：部署环境尚未设置有效的 MERIDIAN_BASE_URL";
  if (/private key|私钥|PKCS|asn1/i.test(message)) return "应用私钥无法识别，请使用官方密钥工具生成的 RSA2 应用私钥";
  if (/public key|公钥|unsupported RSA/i.test(message)) return "支付宝公钥无法识别，请粘贴接口加签配置完成后由支付宝生成的 RSA2 公钥";
  return `保存失败：${message}`;
}

function merchantForm(){openForm("新建商户",'<label>商户名称<input name="name" required maxlength="128"></label><label>处理费率（%）<input name="fee_percent" type="number" value="0" min="0" max="100" step="0.01" required></label>',"创建",async(data)=>{const item=await api('/admin/api/merchants',{method:'POST',body:JSON.stringify({name:data.get('name'),fee_bps:Math.round(Number(data.get('fee_percent'))*100)})});state.merchantId=item.id;toast('商户已创建')})}
function merchantAccessForm(){if(!state.merchantId)return toast('请先选择商户',true);openForm('签发商户登录令牌','<p class="muted">新令牌只显示一次；再次签发会立即使旧令牌失效。</p>','签发并轮换',async()=>{const result=await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/access-token`,{method:'POST',body:'{}'});showSecret('商户登录令牌',`管理令牌\n${result.admin_token}`)})}
function applicationForm(){if(!state.merchantId)return toast('请先创建商户',true);openForm("新建应用",'<label>应用名称<input name="name" required maxlength="128"></label><label>Webhook URL<input name="webhook_url" type="url" placeholder="https://example.com/webhooks/payments"></label>',"创建",async(data)=>{const result=await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/applications`,{method:'POST',body:JSON.stringify({name:data.get('name'),webhook_url:data.get('webhook_url')})});showSecret('应用凭证',`API Key\n${result.api_key}\n\nWebhook Secret\n${result.webhook_secret}`)})}
function editApplicationForm(item){openForm('编辑应用',`<label>应用名称<input name="name" value="${escapeHTML(item.name)}" required maxlength="128"></label><label>Webhook URL<input name="webhook_url" type="url" value="${escapeHTML(item.webhook_url||'')}" placeholder="https://example.com/webhooks/payments"></label><label>状态<select name="status"><option value="active" ${item.status==='active'?'selected':''}>正常</option><option value="disabled" ${item.status==='disabled'?'selected':''}>已停用</option></select></label>`,'保存',async(data)=>{await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/applications/${encodeURIComponent(item.id)}`,{method:'PATCH',body:JSON.stringify({name:data.get('name'),webhook_url:data.get('webhook_url'),status:data.get('status')})});toast('应用已更新')})}
function applicationAction(action,id){const item=state.applications.find((candidate)=>candidate.id===id);if(!item)return;if(action==='edit')return editApplicationForm(item);if(action==='toggle')return openForm(item.status==='active'?'停用应用':'启用应用',`<p class="muted">${item.status==='active'?'停用后该应用 API Key 将立即失效。':'启用后该应用可以继续创建和查询订单。'}</p>`,item.status==='active'?'停用':'启用',async()=>{await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/applications/${encodeURIComponent(item.id)}`,{method:'PATCH',body:JSON.stringify({name:item.name,webhook_url:item.webhook_url,status:item.status==='active'?'disabled':'active'})});toast('应用状态已更新')});if(action==='rotate')return openForm('轮换应用凭证','<p class="muted">新 API Key 和 Webhook Secret 只显示一次，旧凭证会立即失效。</p>','确认轮换',async()=>{const result=await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/applications/${encodeURIComponent(item.id)}/credentials`,{method:'POST',body:'{}'});showSecret('应用凭证',`API Key\n${result.api_key}\n\nWebhook Secret\n${result.webhook_secret}`)})}
function staticCodeForm(){if(!state.merchantId)return toast('请先创建商户',true);openForm("上传静态收款码",'<label>名称<input name="name" required></label><label>门店<input name="store_name"></label><label>二维码图片<input name="image" type="file" accept="image/png,image/jpeg,image/webp" required></label>',"上传",async(data)=>{await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/static-codes`,{method:'POST',body:data});toast('收款码已上传')})}
function alipayForm() {
  if (!state.merchantId) return toast("请先创建商户", true);
  const environment = state.alipay?.environment || "sandbox";
  openForm("配置支付宝账户", `
    <section class="alipay-setup" aria-label="支付宝自助配置">
      <div class="alipay-intro">
        <div><p class="eyebrow">支付宝直连</p><p>按左侧顺序在支付宝完成准备，再把三项内容粘贴到右侧。Meridian 只支持 RSA2 公钥模式。</p></div>
        ${state.alipay ? '<span class="pill active">已配置，重新保存需再次填写密钥</span>' : '<span class="pill pending">首次配置</span>'}
      </div>
      <div class="alipay-layout">
        <aside class="alipay-guide" aria-label="支付宝官方操作步骤">
          <label>运行环境
            <select name="environment" required>
              <option value="sandbox" ${environment === "sandbox" ? "selected" : ""}>沙箱环境</option>
              <option value="production" ${environment === "production" ? "selected" : ""}>正式环境</option>
            </select>
          </label>
          <div id="alipay-environment-guide"></div>
        </aside>
        <div class="alipay-fields">
          <div class="form-section-head"><strong>填写 Meridian 配置</strong><span>所有密钥均在保存时校验，并加密存储。</span></div>
          <label>App ID
            <span class="field-help">从所选环境的应用详情复制，通常为 16 位数字。</span>
            <input name="app_id" value="${escapeHTML(state.alipay?.app_id || "")}" inputmode="numeric" autocomplete="off" placeholder="例如：2021000000000000" required>
          </label>
          <label>应用 RSA2 私钥
            <span class="field-help">粘贴密钥工具生成的“应用私钥”，支持原始密钥文本或完整 PEM。不要粘贴应用公钥。</span>
            <textarea class="key-input mono" name="private_key_pem" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="粘贴应用私钥内容" required></textarea>
          </label>
          <label>支付宝 RSA2 公钥
            <span class="field-help">完成接口加签设置后，复制支付宝页面生成的“支付宝公钥”。它不是应用公钥。</span>
            <textarea class="key-input mono" name="alipay_public_key_pem" autocomplete="off" autocapitalize="off" spellcheck="false" placeholder="粘贴支付宝公钥内容" required></textarea>
          </label>
          <div id="alipay-callback-guide"></div>
        </div>
      </div>
    </section>`, "校验并保存", async(data) => {
      const body = Object.fromEntries(data.entries());
      body.app_id = String(body.app_id || "").trim();
      body.private_key_pem = normalizePEM(body.private_key_pem, "PRIVATE KEY", "应用 RSA2 私钥");
      body.alipay_public_key_pem = normalizePEM(body.alipay_public_key_pem, "PUBLIC KEY", "支付宝 RSA2 公钥");
      try {
        await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/alipay`, {method:"PUT", body:JSON.stringify(body)});
      } catch (error) {
        throw new Error(readableAlipayError(error));
      }
      toast("支付宝账户已校验并保存");
    }, {wide:true});
  const form = $("#dialog-form");
  const environmentSelect = form.elements.environment;
  const updateGuide = () => { $("#alipay-environment-guide").innerHTML = alipayEnvironmentGuide(environmentSelect.value); };
  environmentSelect.addEventListener("change", updateGuide);
  updateGuide();
  $("#alipay-callback-guide").innerHTML = alipayCallbackGuide();
}
function paymentForm(){
  if(!state.merchantId)return toast('请先创建商户',true);
  const apps=state.applications.filter((item)=>item.status==='active');
  if(!apps.length)return toast('请先创建或启用一个应用',true);
  const codes=state.staticCodes.filter((item)=>item.enabled);
  const appOptions=apps.map((item)=>`<option value="${escapeHTML(item.id)}">${escapeHTML(item.name)}</option>`).join('');
  const codeOptions=codes.length?codes.map((item)=>`<option value="${escapeHTML(item.id)}">${escapeHTML(item.name)}</option>`).join(''):'<option value="">暂无可用收款码</option>';
  openForm("创建支付订单",`<label>应用<select name="application_id" required>${appOptions}</select></label><div class="form-grid"><label>商户订单号<input name="merchant_order_id" value="order-${Date.now()}" required></label><label>支付方式<select name="method"><option value="static_alipay">支付宝静态码</option><option value="alipay_qr">支付宝动态码</option><option value="alipay_page">支付宝电脑网站</option><option value="alipay_wap">支付宝手机网站</option></select></label></div><div class="form-grid"><label>金额（元）<input name="amount" type="number" min="0.01" step="0.01" value="0.01" required></label><label>静态收款码<select name="static_code_id">${codeOptions}</select></label></div><label>订单标题<input name="subject" value="Meridian 测试订单" required maxlength="128"></label><label>回跳 URL<input name="return_url" type="url"></label>`,"创建",async(data)=>{const method=data.get('method');const result=await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/payments`,{method:'POST',body:JSON.stringify({application_id:data.get('application_id'),merchant_order_id:data.get('merchant_order_id'),method,amount_minor:Math.round(Number(data.get('amount'))*100),currency:'CNY',subject:data.get('subject'),static_code_id:method==='static_alipay'?data.get('static_code_id'):'',return_url:data.get('return_url')})});showSecret('订单已创建',`<strong>订单已创建</strong>\n\n订单 ID\n${escapeHTML(result.id)}\n\n收银地址\n<a href="${escapeHTML(result.checkout_url)}" target="_blank" rel="noreferrer">${escapeHTML(result.checkout_url)}</a>${result.redirect_url?`\n\n支付宝地址\n<a href="${escapeHTML(result.redirect_url)}" target="_blank" rel="noreferrer">打开支付宝</a>`:''}`,true)});
  const form=$("#dialog-form"),method=form.elements.method,code=form.elements.static_code_id,submit=form.querySelector('button[type="submit"]');
  const sync=()=>{const needsCode=method.value==='static_alipay';code.disabled=!needsCode;code.required=needsCode;submit.disabled=needsCode&&!codes.length;};
  method.addEventListener('change',sync);sync();
}

function paymentDetails(item){const rows=[["订单 ID",item.id],["商户订单号",item.merchant_order_id],["支付方式",labelFor(methodLabels,item.method)],["状态",labelFor(statusLabels,item.status)],["金额",money(item.amount_minor,item.currency)],["渠道状态",item.channel_status||'-'],["渠道订单号",item.channel_order_id||'-'],["已退款",money(item.refunded_minor,item.currency)],["最近错误",item.last_error||'-'],["创建时间",timeText(item.created_at)],["更新时间",timeText(item.updated_at)]];showSecret('订单详情',`<div class="detail-list">${rows.map(([key,value])=>`<div><span>${escapeHTML(key)}</span><strong>${escapeHTML(value)}</strong></div>`).join('')}</div>`,true)}

async function paymentAction(action,id,remaining){
  const item=state.payments.find((candidate)=>candidate.id===id);
  if(action==='details'&&item)return paymentDetails(item);
  const base=`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/payments/${encodeURIComponent(id)}`;
  if(action==='confirm') return openForm('确认到账','<label>确认备注<textarea name="note" maxlength="1000">已核对收款记录</textarea></label>','确认到账',async(data)=>{await api(`${base}/confirm`,{method:'POST',body:JSON.stringify({note:data.get('note')})});toast('订单已确认到账')});
  if(action==='abnormal') return openForm('标记异常','<label>异常备注<textarea name="note" maxlength="1000" required></textarea></label>','标记异常',async(data)=>{await api(`${base}/abnormal`,{method:'POST',body:JSON.stringify({note:data.get('note')})});toast('订单已标记异常')});
  if(action==='close') return openForm('关闭订单','<p class="muted">关闭后该订单将不再接受支付。</p>','关闭订单',async()=>{await api(`${base}/close`,{method:'POST',body:'{}'});toast('订单已关闭')});
  if(action==='refund'){
    const maximum=(Math.max(0,Number(remaining))/100).toFixed(2);
    return openForm('创建退款',`<label>退款金额（元）<input name="amount" type="number" min="0.01" max="${maximum}" step="0.01" value="${maximum}" required></label><label>退款原因<input name="reason" value="admin refund" maxlength="256"></label>`,'提交退款',async(data)=>{await api(`${base}/refund`,{method:'POST',body:JSON.stringify({merchant_refund_id:`refund-${Date.now()}`,amount_minor:Math.round(Number(data.get('amount'))*100),reason:data.get('reason')})});toast('退款已提交')});
  }
  try{if(action==='query'){await api(`${base}/query`,{method:'POST',body:'{}'});toast('查询已完成');await refresh()}}catch(error){toast(error.message,true)}
}

$("#login-form").addEventListener("submit",async(event)=>{event.preventDefault();state.token=$("#admin-token").value.trim();const ok=await refresh();if(ok){sessionStorage.setItem("meridian_admin_token",state.token);showApp()}});
$("#logout").addEventListener("click",()=>{sessionStorage.removeItem("meridian_admin_token");state.token="";state.access=null;$("#admin-token").value="";showLogin()});
$("#merchant-select").addEventListener("change",(event)=>{state.merchantId=event.target.value;loadMerchantData().catch((error)=>toast(error.message,true))});
$("#refresh").addEventListener("click",refresh); $("#status-filter").addEventListener("change",render);
$("#add-merchant").addEventListener("click",merchantForm);$("#rotate-merchant-token").addEventListener("click",merchantAccessForm);$("#add-application").addEventListener("click",applicationForm);$("#add-static-code").addEventListener("click",staticCodeForm);$("#configure-alipay").addEventListener("click",alipayForm);$("#new-payment").addEventListener("click",paymentForm);
document.querySelectorAll(".nav button").forEach((node)=>node.addEventListener("click",()=>setView(node.dataset.view)));document.querySelectorAll("[data-view-jump]").forEach((node)=>node.addEventListener("click",()=>setView(node.dataset.viewJump)));document.querySelectorAll("dialog [data-close]").forEach((node)=>node.addEventListener("click",()=>node.closest("dialog").close()));
$("#payments-table").addEventListener("click",(event)=>{const button=event.target.closest("[data-action]");if(button)paymentAction(button.dataset.action,button.dataset.id,button.dataset.remaining)});
$("#application-table").addEventListener("click",(event)=>{const button=event.target.closest("[data-app-action]");if(button)applicationAction(button.dataset.appAction,button.dataset.id)});
$("#delivery-table").addEventListener("click",async(event)=>{const button=event.target.closest("[data-delivery-retry]");if(!button)return;try{await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/deliveries/${encodeURIComponent(button.dataset.deliveryRetry)}/retry`,{method:'POST',body:'{}'});toast('投递已重新排队');await refresh()}catch(error){toast(error.message,true)}});
$("#static-code-table").addEventListener("click",async(event)=>{const button=event.target.closest("[data-static-toggle]");if(!button)return;try{await api(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/static-codes/${encodeURIComponent(button.dataset.staticToggle)}`,{method:'PATCH',body:JSON.stringify({enabled:button.dataset.enabled!=='true'})});await refresh()}catch(error){toast(error.message,true)}});
$("#reconciliation").addEventListener("click",async(event)=>{event.preventDefault();if(!state.merchantId)return;try{const response=await fetch(`/admin/api/merchants/${encodeURIComponent(state.merchantId)}/reconciliation.csv`,{headers:{Authorization:`Bearer ${state.token}`}});if(!response.ok)throw new Error(`HTTP ${response.status}`);const blob=await response.blob();const href=URL.createObjectURL(blob);const anchor=document.createElement('a');anchor.href=href;anchor.download='meridian-reconciliation.csv';anchor.click();URL.revokeObjectURL(href)}catch(error){toast(error.message,true)}});

checkHealth();
if (state.token) { $("#admin-token").value=state.token; refresh().then((ok)=>{if(ok)showApp();else showLogin()}).catch(()=>showLogin()); }
