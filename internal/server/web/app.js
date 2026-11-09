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
drop.addEventListener("drop", async ev => upload(await droppedFiles(ev.dataTransfer)));
$("#file").onchange = ev => { upload([...ev.target.files]); ev.target.value = ""; };
$("#folder").onchange = ev => { upload([...ev.target.files]); ev.target.value = ""; };
const auto = $("#auto-save");
auto.checked = localStorage.getItem("hdk-auto") !== "0";
auto.onchange = () => localStorage.setItem("hdk-auto", auto.checked ? "1" : "0");

// droppedFiles walks dropped folders; plain files come through unchanged.
async function droppedFiles(dt) {
  const entries = [...dt.items].map(i => i.webkitGetAsEntry && i.webkitGetAsEntry()).filter(Boolean);
  if (!entries.length) return [...dt.files];
  const out = [];
  const walk = async e => {
    if (e.isFile) { out.push(await new Promise((ok, bad) => e.file(ok, bad))); return; }
    const r = e.createReader();
    for (let batch; (batch = await new Promise((ok, bad) => r.readEntries(ok, bad))).length;) for (const x of batch) await walk(x);
  };
  for (const e of entries) await walk(e);
  return out;
}

// Files from a folder: invoice formats, plus .txt or no extension (content is sniffed).
const INVOICE_EXT = /\.(xml|pdf|zip|html?|png|jpe?g|jfif|bmp|gif|tiff?|webp|txt)$|^[^.]+$/i;

async function upload(files) {
  files = files.filter(f => INVOICE_EXT.test(f.name) && f.size < 64 << 20);
  if (!files.length) { toast("Không có file hóa đơn nào (XML, PDF, ZIP, HTML, ảnh)", true); return; }
  const batch = auto.checked;
  const sum = { files: files.length, saved: [], review: 0, dup: [], failed: [] };
  // Gửi từng nhóm nhỏ: không vượt giới hạn tải lên và thấy được tiến độ.
  const chunks = [];
  for (let i = 0, size = 0, cur = []; i < files.length; i++) {
    cur.push(files[i]); size += files[i].size;
    if (cur.length === 20 || size > 24 << 20 || i === files.length - 1) { chunks.push(cur); cur = []; size = 0; }
  }
  let done = 0;
  for (const chunk of chunks) {
    toast(`Đang đọc file ${done + 1}–${done + chunk.length} / ${files.length}...`);
    const fd = new FormData();
    chunk.forEach(f => fd.append("files", f, f.webkitRelativePath || f.name));
    try {
      const r = await api("POST", batch ? "/api/batch" : "/api/parse", fd);
      if (batch) sum.saved.push(...r.saved);
      (batch ? r.drafts : r).forEach(d => {
        if (d.error) sum.failed.push({ fileName: d.fileName, why: d.error });
        else if (batch && d.duplicateOf) { sum.dup.push(d); return; }
        else sum.review++;
        $("#drafts").prepend(draftCard(d));
      });
    } catch (e) { chunk.forEach(f => sum.failed.push({ fileName: f.name, why: e.message })); }
    done += chunk.length;
  }
  if (batch || files.length > 1) showSummary(sum, batch);
}

const SUMMARY_HEAD = ["Kết quả", "File", "Số HĐ", "Ngày", "Người bán / lỗi", "Tổng", "Phiếu nhập"];

function showSummary(sum, batch) {
  const inv = d => d.invoice;
  const rows = [
    ...sum.saved.map(s => ["Đã tự lưu", s.fileName, s.number, dmy(s.date), s.seller, money(s.total), s.receiptNo || "—"]),
    ...sum.dup.map(d => ["Trùng, bỏ qua", d.fileName, inv(d).number, dmy(inv(d).date), inv(d).seller.name, money(inv(d).total * inv(d).exRate), ""]),
    ...sum.failed.map(f => ["Lỗi", f.fileName, "", "", f.why, "", ""]),
  ];
  const csv = () => {
    const q = v => `"${String(v).replace(/"/g, '""')}"`;
    const text = [SUMMARY_HEAD, ...rows].map(r => r.map(q).join(",")).join("\r\n");
    const a = h("a", { href: URL.createObjectURL(new Blob(["﻿" + text], { type: "text/csv" })), download: "ket-qua-nhap-hoa-don.csv" });
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 1000);
  };
  const box = h("div", { class: "summary" },
    h("h3", {}, `Đã đọc ${sum.files} file`),
    h("p", {},
      batch ? h("span", { class: "badge g" }, `${sum.saved.length} hóa đơn tự lưu`) : null,
      h("span", { class: "badge s" }, `${sum.review} cần kiểm tra bên dưới`),
      sum.dup.length ? h("span", { class: "badge d" }, `${sum.dup.length} đã nhập trước đó`) : null,
      sum.failed.length ? h("span", { class: "badge x" }, `${sum.failed.length} lỗi`) : null),
    rows.length ? h("table", { class: "grid" },
      h("thead", {}, h("tr", {}, SUMMARY_HEAD.map(x => h("th", {}, x)))),
      h("tbody", {}, rows.map(r => h("tr", {}, r.map(x => h("td", {}, x)))))) : null,
    h("div", { class: "actions" },
      rows.length ? h("button", { class: "secondary", onclick: csv }, "Tải báo cáo (CSV)") : null,
      h("button", { class: "secondary", onclick: () => box.remove() }, "Đóng")));
  $("#summary").prepend(box);
}

$("#paste-go").onclick = async () => {
  const text = $("#paste-text").value;
  if (!text.trim()) { toast("Hãy dán chữ hóa đơn vào ô trước", true); return; }
  try {
    const drafts = await api("POST", "/api/parse-text", { text });
    drafts.forEach(d => $("#drafts").prepend(draftCard(d)));
    if (drafts.some(d => !d.error)) $("#paste-text").value = "";
  } catch (e) { toast(e.message, true); }
};

const SOURCES = {
  xml: "XML hóa đơn điện tử — dữ liệu gốc, chính xác tuyệt đối",
  pdf: "PDF — đọc từ chữ in, đã đối chiếu bằng phép tính",
  html: "HTML — đọc từ trang hiển thị, đã đối chiếu bằng phép tính",
  ocr: "ẢNH (OCR) — chỉ là bản gợi ý, phải đối chiếu từng dòng với bản gốc",
  "pdf-raw": "PDF lỗi font — thứ tự chữ phải dựng lại, phải đối chiếu từng dòng với bản gốc",
  text: "Chữ dán vào — đã đối chiếu bằng phép tính, cần đối chiếu với hóa đơn gốc",
};

// softOpen counts ⚠ points not yet confirmed (✖ must be fixed, never confirmed in bulk).
function softOpen(inv) {
  const n = (issues, confirmed) => confirmed ? 0 : (issues || []).filter(i => !i.block).length;
  return n(inv.issues, inv.confirmed) + inv.lines.reduce((a, l) => a + n(l.issues, l.confirmed), 0);
}

// unresolved counts what still blocks saving (the server enforces the same rule).
function unresolved(inv) {
  let n = 0;
  const count = (issues, confirmed) => (issues || []).forEach(i => { if (i.block || !confirmed) n++; });
  count(inv.issues, inv.confirmed);
  inv.lines.forEach(l => count(l.issues, l.confirmed));
  return n;
}

function issueList(issues, confirmed, onConfirm) {
  if (!issues || !issues.length) return null;
  const soft = issues.filter(i => !i.block);
  return h("div", { class: "issues" },
    issues.map(i => h("div", { class: i.block ? "blk" : "chk" }, (i.block ? "✖ Phải sửa: " : "⚠ ") + i.text)),
    soft.length ? h("label", { class: "confirm" },
      h("input", { type: "checkbox", checked: !!confirmed, onchange: e => onConfirm(e.target.checked) }),
      " Tôi đã đối chiếu với hóa đơn gốc") : null);
}

function draftCard(d) {
  const card = h("div", { class: "draft" });
  if (d.error) {
    add(card, h("h3", {}, d.fileName), h("div", { class: "err" }, d.error),
      h("div", { class: "actions" }, h("button", { class: "secondary", onclick: () => card.remove() }, "Đóng")));
    return card;
  }
  const inv = d.invoice;
  const counts = h("div");
  const body = h("tbody");
  const totals = h("tfoot");
  const invBox = h("div");
  const status = h("div", { class: "status" });
  const save = h("button", {}, "Lưu & nhập kho");
  const confirmAll = h("button", { class: "secondary" });
  confirmAll.onclick = () => {
    if (!confirm(`Bạn đã đối chiếu cả ${softOpen(inv)} điểm ⚠ của hóa đơn này với bản gốc và thấy đúng?`)) return;
    if ((inv.issues || []).some(i => !i.block)) inv.confirmed = true;
    inv.lines.forEach(l => { if ((l.issues || []).some(i => !i.block)) l.confirmed = true; });
    paint();
  };
  const rows = [];
  let timer;

  const paint = () => {
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
    totals.replaceChildren(h("tr", {}, h("td", { colSpan: 5 }, "Cộng các dòng"), h("td", { class: "n" }, money(sum)),
      h("td"), h("td", { class: "n" }, money(vat)), h("td", { colSpan: 2 }, "Thanh toán: " + money(sum + vat))));
    invBox.replaceChildren();
    add(invBox, issueList(inv.issues, inv.confirmed, v => { inv.confirmed = v; paint(); }));
    rows.forEach(({ l, tr, cell }) => {
      tr.className = l.kind + (l.issues && l.issues.length ? (l.issues.some(i => i.block) || !l.confirmed ? " bad" : " ok") : "");
      cell.replaceChildren();
      add(cell, issueList(l.issues, l.confirmed, v => { l.confirmed = v; paint(); }), h("div", { class: "reason" }, l.reason));
    });
    const n = unresolved(inv);
    status.className = "status " + (n || d.duplicateOf ? "no" : "yes");
    status.textContent = d.duplicateOf ? "Hóa đơn này đã được nhập trước đó: không lưu lại để tránh trùng kho."
      : n ? `Còn ${n} điểm chưa kiểm chứng được: sửa các dòng ✖ và đánh dấu đã đối chiếu các dòng ⚠ rồi mới lưu.`
        : "✔ Mọi số liệu đã khớp hoặc đã được bạn xác nhận: có thể lưu.";
    save.disabled = n > 0 || d.duplicateOf > 0;
    const soft = softOpen(inv);
    confirmAll.hidden = soft < 2;
    confirmAll.textContent = `✔ Xác nhận cả ${soft} điểm ⚠`;
  };

  // recheck asks the server to re-verify after an edit; edited lines lose their confirmation.
  const recheck = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      try {
        const r = await api("POST", "/api/check", inv);
        inv.issues = r.issues || [];
        r.lines.forEach((x, i) => { inv.lines[i].issues = x.issues || []; inv.lines[i].certain = x.certain; });
        paint();
      } catch (e) { toast(e.message, true); }
    }, 350);
  };
  const edited = l => { if (l) l.confirmed = false; else inv.confirmed = false; recheck(); };

  const lineRow = l => {
    const tr = h("tr", { class: l.kind });
    const cell = h("td", { class: "issuecell" });
    const inp = (key, attrs = {}) => h("input", { value: l[key] ?? "", title: l[key] ?? "", ...attrs,
      oninput: e => { l[key] = attrs.type === "number" ? (parseFloat(e.target.value) || 0) : e.target.value; edited(l); } });
    const num = key => inp(key, { type: "number", step: "any", class: "num" });
    const sel = h("select", { onchange: e => { l.kind = e.target.value; l.userSet = true; edited(l); } },
      Object.entries(KINDS).map(([k, v]) => h("option", { value: k, selected: k === l.kind }, v)));
    tr.append(
      h("td", {}, l.stt),
      h("td", { style: "min-width:260px" }, inp("name")),
      h("td", { style: "width:70px" }, inp("unit")),
      h("td", { style: "width:90px" }, num("qty")),
      h("td", { style: "width:120px" }, num("price")),
      h("td", { style: "width:130px" }, num("amount")),
      h("td", { style: "width:70px" }, inp("vatRate")),
      h("td", { style: "width:110px" }, num("vatAmount")),
      h("td", {}, sel),
      cell);
    rows.push({ l, tr, cell });
    return tr;
  };
  const addLine = () => {
    const l = { stt: inv.lines.length + 1, nature: 1, name: "", unit: "", qty: 1, price: 0, amount: 0, vatRate: "8%",
      vatAmount: 0, kind: "goods", reason: "Dòng thêm tay", userSet: true, issues: [] };
    inv.lines.push(l);
    body.append(lineRow(l));
    edited(l);
  };
  inv.lines.forEach(l => body.append(lineRow(l)));

  save.onclick = async () => {
    save.disabled = true;
    try {
      const r = await api("POST", "/api/invoices", { token: d.token, invoice: inv });
      toast(r.receiptNo ? `Đã lưu hóa đơn số ${inv.number}, phiếu nhập kho ${r.receiptNo}` : `Đã lưu hóa đơn số ${inv.number} (không có hàng hóa nhập kho)`);
      card.remove();
    } catch (e) { toast(e.message, true); paint(); }
  };

  const hf = (label, obj, key, opts = {}) => h("label", { class: opts.wide ? "wide" : "" }, label,
    h("input", { value: obj[key] ?? "", type: opts.type || "text", oninput: e => { obj[key] = e.target.value; edited(null); } }));
  add(card,
    h("h3", {}, d.fileName),
    h("div", { class: "source src-" + inv.source }, "Nguồn: " + (SOURCES[inv.source] || inv.source)),
    h("div", { class: "head" },
      hf("Ký hiệu mẫu số", inv, "form"), hf("Ký hiệu", inv, "serial"), hf("Số hóa đơn", inv, "number"),
      hf("Ngày hóa đơn", inv, "date", { type: "date" }),
      hf("Người bán", inv.seller, "name", { wide: true }), hf("MST người bán", inv.seller, "taxCode"),
      hf("MST người mua", inv.buyer, "taxCode"), hf("Tiền tệ", inv, "currency")),
    (d.warnings || []).map(w => h("div", { class: "warn" }, w)),
    invBox,
    counts,
    h("table", { class: "grid" },
      h("thead", {}, h("tr", {}, ["STT", "Tên hàng hóa, dịch vụ", "ĐVT", "Số lượng", "Đơn giá", "Thành tiền", "Thuế suất", "Tiền thuế", "Phân loại", "Kiểm chứng"]
        .map((t, i) => h("th", { class: [3, 4, 5, 7].includes(i) ? "n" : "" }, t)))),
      body, totals),
    status,
    h("div", { class: "actions" },
      h("span", { class: "muted" }, "Xanh lá = vào kho, xanh dương = chi phí. Phân loại bạn chọn sẽ được nhớ cho lần sau."),
      h("button", { class: "secondary", onclick: addLine }, "+ Thêm dòng"),
      h("button", { class: "secondary", onclick: () => card.remove() }, "Bỏ qua"),
      confirmAll,
      save));
  paint();
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
    ocrStatus();
  } catch (e) { toast(e.message, true); }
};
async function ocrStatus() {
  try {
    const o = await api("GET", "/api/ocr");
    $("#ocr-status").textContent = o.error ? "Không dùng được OCR: " + o.error
      : o.vietnamese ? "✔ Đã có OCR tiếng Việt: ảnh chụp đọc được đủ dấu."
        : `Chưa có OCR tiếng Việt (đang có: ${(o.langs || []).join(", ") || "không"}): tên hàng đọc từ ảnh sẽ mất dấu.`;
    $("#ocr-install").hidden = !!(o.vietnamese || o.error);
  } catch (e) { $("#ocr-status").textContent = e.message; }
}
$("#ocr-install").onclick = async () => {
  try { await api("POST", "/api/ocr/install-vi"); toast("Đã mở cửa sổ cài đặt. Cài xong, mở lại trang Cài đặt để kiểm tra."); }
  catch (e) { toast(e.message, true); }
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
