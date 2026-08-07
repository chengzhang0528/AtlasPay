const body = document.body;
const amount = Number(body.dataset.amount || 0);
document.querySelector("#amount").textContent = new Intl.NumberFormat("zh-CN", {style:"currency", currency:body.dataset.currency || "CNY"}).format(amount / 100);

function guidance(status, payable) {
  const terminal = {paid:"商户已确认到账。",closed:"订单已关闭。",failed:"支付未完成，请联系商户。",refunding:"退款处理中。",partially_refunded:"订单已部分退款。",refunded:"订单已退款。"};
  if (terminal[status]) return terminal[status];
  if (!payable) return "订单已过期，正在关闭。";
  return body.dataset.method === "static_alipay" ? "等待商户操作员核对到账。" : "等待支付宝确认支付结果。";
}

let pollTimer;
let polling = false;

async function poll() {
  if (polling) return;
  polling = true;
  let keepPolling = true;
  try {
    const response = await fetch(body.dataset.statusUrl, {cache:"no-store"});
    if (response.ok) {
      const data = await response.json();
      const badge = document.querySelector("#status");
      badge.textContent = data.status; badge.className = `pill ${data.status}`;
      document.querySelector("#status-text").textContent = data.status;
      document.querySelector("#status-guidance").textContent = guidance(data.status, data.payable);
      if (!data.payable) document.querySelectorAll("[data-payment-action]").forEach((node) => { node.hidden = true; });
      keepPolling = data.payable && ["created","pending"].includes(data.status);
    }
  } catch (_) {}
  finally { polling = false; }
  if (keepPolling) pollTimer = setTimeout(poll, 3000);
}

function refreshStatus() {
  clearTimeout(pollTimer);
  poll();
}

window.addEventListener("pageshow", refreshStatus);
document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") refreshStatus(); });
refreshStatus();
