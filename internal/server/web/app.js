"use strict";

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const nf = new Intl.NumberFormat("vi-VN", { maximumFractionDigits: 2 });
const money = v => nf.format(Math.round(v || 0));
const qty = v => nf.format(v || 0);
const dmy = d => (d && d.length === 10 ? `${d.slice(8)}/${d.slice(5, 7)}/${d.slice(0, 4)}` : d || "");
const KINDS = { goods: "Hàng hóa → kho", service: "Dịch vụ → chi phí", discount: "Chiết khấu", note: "Ghi chú (bỏ qua)" };

// h builds DOM safely: text is always set via textContent.
function h(tag, attrs = {}, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (v === undefined || v === null || v === false) continue;
    if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "class") el.className = v;
    else if (k in el && k !== "list") el[k] = v;
    else el.setAttribute(k, v);
  }
  return add(el, ...kids);
}

// add appends children, skipping null/false and flattening arrays.
function add(el, ...kids) {
  for (const c of kids.flat(Infinity)) if (c !== null && c !== undefined && c !== false) el.append(c instanceof Node ? c : String(c));
  return el;
}

async function api(method, url, body) {
  const opt = { method, headers: { "X-App": "hoadon-kho" } };
  if (body instanceof FormData) opt.body = body;
  else if (body !== undefined) { opt.body = JSON.stringify(body); opt.headers["Content-Type"] = "application/json"; }
  const r = await fetch(url, opt);
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || r.statusText);
  return data;
}

function toast(msg, bad) {
  const t = h("div", { class: bad ? "bad" : "" }, msg);
  $("#toast").append(t);
  setTimeout(() => t.remove(), bad ? 8000 : 4000);
}

function modal(...content) {
  const body = $("#modal-body");
  body.replaceChildren();
  add(body, ...content);
  $("#modal").hidden = false;
}
$("#modal-close").onclick = () => ($("#modal").hidden = true);

// ---------- tabs ----------
const loaders = {};
$$("nav button").forEach(b => (b.onclick = () => {
  $$("nav button").forEach(x => x.classList.toggle("active", x === b));
  $$(".tab").forEach(t => t.classList.toggle("active", t.id === "tab-" + b.dataset.tab));
  loaders[b.dataset.tab]?.();
}));

// ---------- 1. upload & review ----------
const drop = $("#drop");
["dragenter", "dragover"].forEach(e => drop.addEventListener(e, ev => { ev.preventDefault(); drop.classList.add("over"); }));
["dragleave", "drop"].forEach(e => drop.addEventListener(e, ev => { ev.preventDefault(); drop.classList.remove("over"); }));
drop.addEventListener("drop", ev => upload(ev.dataTransfer.files));
$("#file").onchange = ev => { upload(ev.target.files); ev.target.value = ""; };

async function upload(files) {
  if (!files.length) return;
  const fd = new FormData();
  for (const f of files) fd.append("files", f);
  toast(`Đang đọc ${files.length} file...`);
  try {
    const drafts = await api("POST", "/api/parse", fd);
    drafts.forEach(d => $("#drafts").prepend(draftCard(d)));
  } catch (e) { toast(e.message, true); }
}

function field(label, obj, key, opts = {}) {
  return h("label", { class: opts.wide ? "wide" : "" }, label,
    h("input", { value: obj[key] ?? "", type: opts.type || "text", oninput: e => (obj[key] = e.target.value) }));
}

function numInput(obj, key, after) {
  return h("input", { class: "num", type: "number", step: "any", value: obj[key] ?? 0,
    oninput: e => { obj[key] = parseFloat(e.target.value) || 0; after?.(); } });
}

function draftCard(d) {
  const card = h("div", { class: "draft" });
  if (d.error) {
    card.append(h("h3", {}, d.fileName), h("div", { class: "err" }, d.error),
      h("div", { class: "actions" }, h("button", { class: "secondary", onclick: () => card.remove() }, "Đóng")));
    return card;
  }
  const inv = d.invoice;
  const counts = h("div");
  const body = h("tbody");
  const totals = h("tfoot");
  const render = () => {
    const c = { goods: 0, service: 0, discount: 0 };
    let sum = 0, vat = 0;
    inv.lines.forEach(l => {
      if (c[l.kind] !== undefined) c[l.kind]++;
      if (l.kind === "note") return;
      const s = l.kind === "discount" ? -1 : 1;
      sum += s * Math.abs(l.amount); vat += s * Math.abs(l.vatAmount);
    });
    counts.replaceChildren();
    add(counts,
      h("span", { class: "badge g" }, `${c.goods} hàng hóa`),
      h("span", { class: "badge s" }, `${c.service} dịch vụ`),
      c.discount ? h("span", { class: "badge d" }, `${c.discount} chiết khấu`) : null);
    totals.replaceChildren(h("tr", {}, h("td", { colSpan: 6 }, "Cộng"), h("td", { class: "n" }, money(sum)),
      h("td"), h("td", { class: "n" }, money(vat)), h("td", { colSpan: 2 }, "Thanh toán: " + money(sum + vat))));
  };
  const lineRow = l => {
    const tr = h("tr", { class: l.kind });
    const sel = h("select", { onchange: e => { l.kind = e.target.value; l.userSet = true; tr.className = l.kind; render(); } },
      Object.entries(KINDS).map(([k, v]) => h("option", { value: k, selected: k === l.kind }, v)));
    tr.append(
      h("td", {}, l.stt),
      h("td", { style: "min-width:300px" }, h("input", { value: l.name, title: l.name, oninput: e => (l.name = e.target.value) })),
      h("td", { style: "width:70px" }, h("input", { value: l.unit, oninput: e => (l.unit = e.target.value) })),
      h("td", { style: "width:90px" }, numInput(l, "qty")),
      h("td", { style: "width:120px" }, numInput(l, "price")),
      h("td", { style: "width:130px" }, numInput(l, "amount", render)),
      h("td", { class: "n" }, money(l.amount)),
      h("td", { style: "width:70px" }, h("input", { value: l.vatRate, oninput: e => (l.vatRate = e.target.value) })),
      h("td", { style: "width:110px" }, numInput(l, "vatAmount", render)),
      h("td", {}, sel),
      h("td", { class: "reason" }, l.reason));
    return tr;
  };
  const addLine = () => {
    const l = { stt: inv.lines.length + 1, nature: 1, name: "", unit: "", qty: 1, price: 0, amount: 0, vatRate: "8%", vatAmount: 0, kind: "goods", reason: "Thêm tay", userSet: true };
    inv.lines.push(l);
    body.append(lineRow(l));
    render();
  };
  inv.lines.forEach(l => body.append(lineRow(l)));
  render();

  const save = h("button", {}, "Lưu & nhập kho");
  save.disabled = d.duplicateOf > 0;
  save.onclick = async () => {
    save.disabled = true;
    try {
      const r = await api("POST", "/api/invoices", { token: d.token, invoice: inv });
      toast(r.receiptNo ? `Đã lưu hóa đơn số ${inv.number}, phiếu nhập kho ${r.receiptNo}` : `Đã lưu hóa đơn số ${inv.number} (không có hàng hóa nhập kho)`);
      card.remove();
    } catch (e) { toast(e.message, true); save.disabled = false; }
  };

  add(card,
    h("h3", {}, `${d.fileName}`),
    h("div", { class: "head" },
      field("Ký hiệu mẫu số", inv, "form"), field("Ký hiệu", inv, "serial"), field("Số hóa đơn", inv, "number"),
      field("Ngày hóa đơn", inv, "date", { type: "date" }),
      field("Người bán", inv.seller, "name", { wide: true }), field("MST người bán", inv.seller, "taxCode"),
      field("Tiền tệ", inv, "currency")),
    d.duplicateOf ? h("div", { class: "err" }, "Hóa đơn này đã được nhập trước đó, không lưu lại để tránh trùng kho.") : null,
    (d.warnings || []).map(w => h("div", { class: "warn" }, w)),
    counts,
    h("table", { class: "grid" },
      h("thead", {}, h("tr", {}, ["STT", "Tên hàng hóa, dịch vụ", "ĐVT", "Số lượng", "Đơn giá", "Thành tiền", "", "Thuế suất", "Tiền thuế", "Phân loại", "Lý do"]
        .map((t, i) => h("th", { class: [3, 4, 5, 6, 8].includes(i) ? "n" : "" }, t)))),
      body, totals),
    h("div", { class: "actions" },
      h("span", { class: "muted" }, "Xanh lá = vào kho, xanh dương = chi phí. Đổi phân loại thì lần sau app tự nhớ."),
      h("button", { class: "secondary", onclick: addLine }, "+ Thêm dòng"),
      h("button", { class: "secondary", onclick: () => card.remove() }, "Bỏ qua"),
      save));
  return card;
}

// ---------- 2. invoices ----------
const iso = d => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

loaders.invoices = async () => {
  try {
    const q = new URLSearchParams({ from: $("#inv-from").value, to: $("#inv-to").value });
    const list = await api("GET", "/api/invoices?" + q);
    const t = $("#inv-table");
    t.replaceChildren(h("thead", {}, h("tr", {}, ["Ngày", "Ký hiệu", "Số", "Người bán", "MST", "Hàng hóa", "Dịch vụ", "Tiền hàng", "Thuế", "Tổng", "Phiếu nhập", ""]
      .map((x, i) => h("th", { class: [7, 8, 9].includes(i) ? "n" : "" }, x)))));
    const tb = h("tbody");
    if (!list.length) tb.append(h("tr", {}, h("td", { colSpan: 12, class: "muted" }, "Chưa có hóa đơn nào trong khoảng này.")));
    list.forEach(inv => {
      const g = inv.lines.filter(l => l.kind === "goods").length, s = inv.lines.filter(l => l.kind === "service").length;
      tb.append(h("tr", {},
        h("td", {}, dmy(inv.date)), h("td", {}, inv.form + inv.serial), h("td", {}, inv.number), h("td", {}, inv.seller.name),
        h("td", {}, inv.seller.taxCode), h("td", {}, g), h("td", {}, s),
        h("td", { class: "n" }, money(inv.totalBeforeVat * inv.exRate)), h("td", { class: "n" }, money(inv.totalVat * inv.exRate)),
        h("td", { class: "n" }, money(inv.total * inv.exRate)), h("td", {}, inv.receiptNo || "—"),
        h("td", { class: "outform" },
          h("button", { class: "secondary", onclick: () => showInvoice(inv) }, "Xem"),
          inv.receiptNo ? h("button", { class: "secondary", onclick: () => (location.href = `/api/export/receipts?invoice=${inv.id}`) }, "Phiếu nhập") : null,
          inv.sourceFile ? h("button", { class: "secondary", onclick: () => window.open(`/api/invoices/${inv.id}/file`) }, "File gốc") : null,
          h("button", { class: "danger", onclick: () => delInvoice(inv) }, "Xóa"))));
    });
    t.append(tb);
  } catch (e) { toast(e.message, true); }
};
$("#inv-load").onclick = loaders.invoices;

function showInvoice(inv) {
  modal(h("h3", {}, `Hóa đơn ${inv.form}${inv.serial} số ${inv.number} ngày ${dmy(inv.date)}`),
    h("p", {}, `${inv.seller.name} — MST ${inv.seller.taxCode}`),
    h("table", { class: "grid" },
      h("thead", {}, h("tr", {}, ["STT", "Tên", "ĐVT", "SL", "Đơn giá", "Thành tiền", "Thuế suất", "Loại", "Mã kho", "Giá trị nhập kho"].map(x => h("th", {}, x)))),
      h("tbody", {}, inv.lines.map(l => h("tr", { class: l.kind },
        h("td", {}, l.stt), h("td", {}, l.name), h("td", {}, l.unit), h("td", { class: "n" }, qty(l.qty)), h("td", { class: "n" }, money(l.price)),
        h("td", { class: "n" }, money(l.amount)), h("td", {}, l.vatRate), h("td", {}, KINDS[l.kind]), h("td", {}, l.itemCode || ""),
        h("td", { class: "n" }, l.kind === "goods" ? money(l.cost) : ""))))));
}

async function delInvoice(inv) {
  if (!confirm(`Xóa hóa đơn số ${inv.number} của ${inv.seller.name}? Hàng đã nhập kho từ hóa đơn này cũng bị xóa.`)) return;
  try { await api("DELETE", `/api/invoices/${inv.id}`); toast("Đã xóa"); loaders.invoices(); }
  catch (e) { toast(e.message, true); }
}

// ---------- 3. stock ----------
let stockRows = [];
loaders.stock = async () => {
  try { stockRows = await api("GET", "/api/stock"); renderStock(); } catch (e) { toast(e.message, true); }
};
$("#stock-q").oninput = () => renderStock();

function renderStock() {
  const q = $("#stock-q").value.trim().toLowerCase();
  const rows = stockRows.filter(r => !q || (r.code + " " + r.name).toLowerCase().includes(q));
  const t = $("#stock-table");
  t.replaceChildren(h("thead", {}, h("tr", {}, ["Mã", "Tên hàng", "ĐVT", "Tổng nhập", "Tổng xuất", "Tồn", "Giá vốn BQ", "Giá trị tồn", ""]
    .map((x, i) => h("th", { class: i >= 3 && i <= 7 ? "n" : "" }, x)))));
  const tb = h("tbody");
  let total = 0;
  if (!rows.length) tb.append(h("tr", {}, h("td", { colSpan: 9, class: "muted" }, "Kho trống. Hãy nhập hóa đơn có hàng hóa.")));
  rows.forEach(r => {
    total += r.closeVal;
    const out = h("td", { class: "outform" });
    const openForm = () => {
      const f = { itemId: r.itemId, date: iso(new Date()), qty: 1, note: "" };
      out.replaceChildren(
        h("input", { type: "date", value: f.date, oninput: e => (f.date = e.target.value) }),
        h("input", { type: "number", step: "any", min: 0, value: 1, style: "width:80px", oninput: e => (f.qty = parseFloat(e.target.value) || 0) }),
        h("input", { placeholder: "Lý do (bán cho..., dùng cho...)", oninput: e => (f.note = e.target.value) }),
        h("button", { onclick: async () => {
          try { const res = await api("POST", "/api/stockout", f); toast(`Đã xuất kho, phiếu ${res.docNo}`); loaders.stock(); }
          catch (e) { toast(e.message, true); } } }, "Xuất"),
        h("button", { class: "secondary", onclick: () => renderStock() }, "Hủy"));
    };
    out.append(h("button", { class: "secondary", onclick: openForm, disabled: r.closeQty <= 0 }, "Xuất kho"),
      h("button", { class: "secondary", onclick: () => showLedger(r) }, "Thẻ kho"));
    tb.append(h("tr", {}, h("td", {}, r.code), h("td", {}, r.name), h("td", {}, r.unit),
      h("td", { class: "n" }, qty(r.inQty)), h("td", { class: "n" }, qty(r.outQty)), h("td", { class: "n" }, qty(r.closeQty)),
      h("td", { class: "n" }, money(r.avgCost)), h("td", { class: "n" }, money(r.closeVal)), out));
  });
  t.append(tb);
  $("#stock-total").textContent = `${rows.length} mặt hàng — tổng giá trị tồn: ${money(total)} đ`;
}

async function showLedger(item) {
  try {
    const ms = await api("GET", `/api/items/${item.itemId}/ledger`);
    modal(h("h3", {}, `Thẻ kho: ${item.code} — ${item.name} (${item.unit})`),
      h("table", { class: "grid" },
        h("thead", {}, h("tr", {}, ["Ngày", "Chứng từ", "Diễn giải", "Nhập", "Xuất", "Tồn", "Giá trị", "Giá trị tồn", ""].map(x => h("th", {}, x)))),
        h("tbody", {}, ms.map(m => h("tr", {},
          h("td", {}, dmy(m.date)), h("td", {}, m.docNo), h("td", {}, m.note),
          h("td", { class: "n" }, m.dir === "IN" ? qty(m.qty) : ""), h("td", { class: "n" }, m.dir === "OUT" ? qty(m.qty) : ""),
          h("td", { class: "n" }, qty(m.balQty)), h("td", { class: "n" }, money(m.amount)), h("td", { class: "n" }, money(m.balValue)),
          h("td", {}, m.dir === "OUT" ? h("button", { class: "danger", onclick: async () => {
            if (!confirm(`Xóa phiếu xuất ${m.docNo}?`)) return;
            try { await api("DELETE", `/api/movements/${m.id}`); $("#modal").hidden = true; loaders.stock(); }
            catch (e) { toast(e.message, true); } } }, "Xóa") : null))))));
  } catch (e) { toast(e.message, true); }
}

// ---------- 4. reports ----------
function setRange(kind) {
  const d = new Date();
  let a, b;
  if (kind === "month") { a = new Date(d.getFullYear(), d.getMonth(), 1); b = new Date(d.getFullYear(), d.getMonth() + 1, 0); }
  if (kind === "quarter") { const q = Math.floor(d.getMonth() / 3) * 3; a = new Date(d.getFullYear(), q, 1); b = new Date(d.getFullYear(), q + 3, 0); }
  if (kind === "year") { a = new Date(d.getFullYear(), 0, 1); b = new Date(d.getFullYear(), 11, 31); }
  $("#rep-from").value = iso(a);
  $("#rep-to").value = iso(b);
}
$$("[data-range]").forEach(b => (b.onclick = () => setRange(b.dataset.range)));
$$("[data-export]").forEach(c => (c.onclick = () => {
  const q = new URLSearchParams({ from: $("#rep-from").value, to: $("#rep-to").value });
  location.href = `/api/export/${c.dataset.export}?${q}`;
}));
setRange("month");

// ---------- settings ----------
loaders.settings = async () => {
  try {
    const s = await api("GET", "/api/settings");
    $$("#settings input[name]").forEach(i => (i.value = s[i.name] ?? ""));
    $("#data-dir").textContent = s.data_dir;
    $("#version").textContent = s.version;
  } catch (e) { toast(e.message, true); }
};
$("#basis-pick").onchange = e => { if (e.target.value) $("#settings [name=form_basis]").value = e.target.value; };
$("#settings").onsubmit = async ev => {
  ev.preventDefault();
  const m = {};
  $$("#settings input[name]").forEach(i => (m[i.name] = i.value));
  try { await api("PUT", "/api/settings", m); toast("Đã lưu cài đặt"); } catch (e) { toast(e.message, true); }
};

// Nhắc điền thông tin doanh nghiệp lần đầu.
api("GET", "/api/settings").then(s => {
  if (!s.company_name) toast("Lần đầu dùng: vào Cài đặt điền tên doanh nghiệp để in lên phiếu nhập kho.");
}).catch(() => {});
