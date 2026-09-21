"use strict";
const $ = id => document.getElementById(id);
let session, active, draft, caps, selected = 0, reviewed = "", busy = false;
let draftChanged = false;
let togglePending = false, statusGeneration = 0, toastTimer;
const headerDrafts = new WeakMap();
const selectorKinds = new WeakMap();
const pathKinds = new WeakMap();
const clone = value => JSON.parse(JSON.stringify(value));
const pretty = value => JSON.stringify(value, null, 2);
function message(text = "") { $("message").textContent = text; }
function notify(text, kind = "success") {
  clearTimeout(toastTimer);
  $("toast").dataset.kind = kind;
  $("toastText").setAttribute("role", kind === "error" ? "alert" : "status");
  $("toastText").textContent = text;
  $("toast").hidden = false;
  if (kind === "success") toastTimer = setTimeout(() => $("toast").hidden = true, 5000);
}
function reportError(error) {
  message(error.message);
  notify(error.message, "error");
}
function injectionState(enabled) {
  const text = enabled ? "Injection ON" : "Injection OFF";
  $("injectionBadge").textContent = text;
  $("injectionBadge").dataset.state = enabled ? "on" : "off";
  $("injectionBadge").hidden = !session;
}
function faultPhases(f, protocol) {
  if (caps.protocol_phases?.[protocol]) return caps.protocol_phases[protocol];
  if (f.Action === "hold_response" || ["truncate", "throttle"].includes(f.Action) && f.Direction === "response") return ["after_upstream_headers"];
  if (["respond", "hold_request", "truncate", "throttle"].includes(f.Action)) return ["before_upstream_request"];
  return caps.phases;
}
async function api(path, body) {
  const response = await fetch("/api/" + path, {method: body === undefined ? "GET" : "POST", headers: body === undefined ? {} : {"Content-Type":"application/json", "X-CSRF-Token":session?.csrf || ""}, body: body === undefined ? undefined : JSON.stringify(body)});
  const data = await response.json();
  if (!response.ok) {
    if (response.status === 401) { session = undefined; $("injectionBadge").hidden = true; $("workspace").hidden = true; $("loginPanel").hidden = false; $("logout").hidden = true; $("identity").textContent = ""; }
    if (response.status === 409 && data.active) { active = data.active; reviewed = ""; showDiff(); }
    throw new Error(data.error || "Request failed");
  }
  return data;
}
function editor() { return session?.role === "editor"; }
function edited() { draftChanged = true; reviewed = ""; $("validation").textContent = "Draft changed. Validate before applying."; showDiff(); }
function showDiff() {
  if (!draft || !active) return;
  $("revision").textContent = `Draft base: ${draft.base_revision} · Active: ${active.revision}`;
  renderRuleDiff($("activeDiff"), $("draftDiff"), active.proxies, draft.proxies);
  $("rebase").hidden = !editor() || draft.base_revision === active.revision;
  $("apply").disabled = !editor() || busy || reviewed !== JSON.stringify(draft) || draft.base_revision !== active.revision;
}
function element(tag, text, parent) { const el = document.createElement(tag); if (text !== undefined) el.textContent = text; if (parent) parent.append(el); return el; }
function field(parent, label, value, change, options) {
  const wrap = element("label", label, parent);
  const input = element(options ? "select" : "input", undefined, wrap);
  if (options) for (const choice of options) { const option = element("option", choice, input); option.value = choice; }
  input.value = value ?? ""; input.disabled = !editor();
  input.addEventListener(options ? "change" : "input", () => { change(input.value); edited(); });
  return input;
}
function defaultFault(action, protocol) {
  const f = {Action:action, Phase: action === "hold_response" ? "after_upstream_headers" : "before_upstream_request"};
  if (["postgresql", "mysql"].includes(protocol)) f.Phase = "after_commit";
  if (action === "delay") f.Duration = 100000000;
  if (action.startsWith("hold_")) f.MaxDuration = 5000000000;
  if (action === "respond") { f.Status = 503; f.Body = "Injected fault"; }
  if (action === "truncate" || action === "throttle") { f.Direction = "request"; if (action === "truncate") f.Bytes = 0; else f.BytesPerSecond = 1024; }
  return f;
}
function renderRules() {
  const p = draft.proxies[selected], info = active.proxies.find(x => x.id === p.id);
  if (!info) { $("rules").replaceChildren(); $("proxyInfo").textContent="This proxy was removed by the operator. Copy any draft edits you need, then discard and load active."; return; }
  $("proxyInfo").textContent = `${info.protocol} · ${info.listen} → ${info.upstream}`;
  $("rules").replaceChildren();
  if (!p.rules.length) element("p", "No rules. Traffic passes through until a rule is added and injection is enabled.", $("rules"));
  p.rules.forEach((r, index) => {
    const box = element("div", undefined, $("rules")); box.className = "rule";
    const title = element("div", undefined, box); title.className = "row";
    element("h3", `Rule ${index + 1}`, title);
    const buttons = element("div", undefined, title);
    for (const [text, offset] of [["↑", -1], ["↓", 1], ["Remove", 0]]) {
      const button = element("button", text, buttons); button.disabled = !editor() || offset !== 0 && (index + offset < 0 || index + offset >= p.rules.length);
      button.setAttribute("aria-label", `${text === "Remove" ? "Remove" : offset < 0 ? "Move up" : "Move down"} rule ${index + 1}`);
      button.onclick = () => { if (!offset) p.rules.splice(index,1); else [p.rules[index],p.rules[index+offset]]=[p.rules[index+offset],p.rules[index]]; edited(); renderRules(); };
    }
    const grid = element("div", undefined, box); grid.className = "fields";
    field(grid, "Rule ID", r.ID, v => r.ID = v);
    field(grid, "Enabled", String(r.Enabled), v => r.Enabled = v === "true", ["true", "false"]);
    const selector = selectorKinds.get(r) || ["Probability", "Nth", "Every"].find(k => r.Select[k] != null) || "Probability";
    selectorKinds.set(r, selector);
    field(grid, "Selector", selector, v => { selectorKinds.set(r,v); r.Select = {[v]: 1}; renderRules(); }, caps.selectors);
    const number = field(grid, selector === "Probability" ? "Probability (0–1; 1 = 100%)" : `${selector} eligible ${["postgresql", "mysql"].includes(info.protocol) ? "commit" : "request"}`, r.Select[selector], v => r.Select[selector] = v === "" ? null : Number(v)); number.type = "number"; number.step = selector === "Probability" ? "any" : "1";
    if (!["postgresql", "mysql"].includes(info.protocol)) {
      field(grid, info.protocol === "grpc" ? "RPC method (blank = any)" : "HTTP method (blank = any)", r.Match.Method, v => r.Match.Method = v);
      element("p", "Method filters which requests receive faults. A blank method matches all methods; forwarding preserves the original method.", box);
      const pathKind = pathKinds.get(r) || (r.Match.PathPattern ? "Pattern" : r.Match.Path ? "Exact" : "Any");
      pathKinds.set(r, pathKind);
      field(grid, "Path match", pathKind, v => { pathKinds.set(r, v); r.Match.Path = ""; r.Match.PathPattern = ""; renderRules(); }, ["Any", "Exact", "Pattern"]);
      if (pathKind !== "Any") {
        const key = pathKind === "Pattern" ? "PathPattern" : "Path";
        const input = field(grid, pathKind === "Pattern" ? "Path pattern (e.g. /payment/:id)" : "Exact path", r.Match[key], v => r.Match[key] = v);
        input.required = true;
      }
      if (pathKind === "Pattern") element("p", ":name matches one nonempty segment, including history. Rules run in order; put exact exceptions first. No wildcards.", box);
      if (info.protocol === "grpc") field(grid, "gRPC service (optional)", r.Match.Service, v => r.Match.Service = v);
      const headerLabel = element("label", "Headers / metadata (JSON object)", box);
      const headerInput = element("textarea", undefined, headerLabel); headerInput.value = headerDrafts.get(r) ?? pretty(r.Match.Headers || {}); headerInput.disabled = !editor();
      headerInput.oninput = () => { headerDrafts.set(r, headerInput.value); try { const h = JSON.parse(headerInput.value); if (!h || Array.isArray(h) || typeof h !== "object" || Object.values(h).some(v => typeof v !== "string")) throw Error(); r.Match.Headers = h; headerInput.setCustomValidity(""); } catch { headerInput.setCustomValidity("Use a JSON object with string values"); } edited(); };
    } else element("p", "Matches confirmed commits of explicit transactions. Selectors count eligible commit cycles; faults can hide the acknowledgment even though data was committed.", box);
    field(grid, "Fault action", r.Fault.Action, v => { r.Fault = defaultFault(v, info.protocol); renderRules(); }, caps.actions[info.protocol]);
    const phases = faultPhases(r.Fault, info.protocol);
    const phase = field(grid, "Phase", r.Fault.Phase, v => r.Fault.Phase = v, phases);
    phase.disabled = !editor() || phases.length === 1;
    if (phases.length === 1) phase.title = "Determined by fault action and direction";
    if (["truncate","throttle"].includes(r.Fault.Action)) field(grid, "Direction", r.Fault.Direction, v => { r.Fault.Direction = v; r.Fault.Phase = v === "request" ? "before_upstream_request" : "after_upstream_headers"; renderRules(); }, ["request","response"]);
    for (const [key,label,scale] of [["Duration","Delay (milliseconds)",1e6],["MaxDuration","Maximum hold (milliseconds)",1e6],["Status","HTTP status",1],["Bytes","Bytes before truncation",1],["BytesPerSecond","Bytes per second",1]]) {
      const parameter = {delay:"Duration",hold_request:"MaxDuration",hold_response:"MaxDuration",respond:"Status",truncate:"Bytes",throttle:"BytesPerSecond"}[r.Fault.Action];
      if (key !== parameter) continue;
      const input = field(grid, label, r.Fault[key] == null ? "" : r.Fault[key]/scale, v => r.Fault[key] = v === "" ? null : Number(v)*scale); input.type="number"; input.step=scale===1 ? "1" : "any";
    }
    if (r.Fault.Action === "respond") field(grid, "Response body", r.Fault.Body || "", v => r.Fault.Body = v);
  });
}
function render() {
  $("proxy").replaceChildren();
  draft.proxies.forEach((p,i) => { const option = element("option",p.id,$("proxy")); option.value=i; });
  selected = Math.min(selected,draft.proxies.length-1); $("proxy").value=selected;
  for (const id of ["enable","disable","add","validate","discard"]) $(id).disabled=!editor();
  renderRules(); showDiff();
}
async function loadActive() { active = await api("config"); }
function resetDraft() { draftChanged=false; draft={base_revision:active.revision,proxies:active.proxies.map(p=>({id:p.id,rules:clone(p.rules)}))}; reviewed=""; render(); }
async function signedIn() {
  $("loginPanel").hidden=true; $("workspace").hidden=false; $("logout").hidden=false;
  $("identity").textContent=`${session.name} · ${session.role}`;
  $("injectionBadge").hidden=false;
  caps=await api("capabilities"); await loadActive(); if (!draft || !draftChanged) resetDraft(); else { reviewed=""; render(); message("Your unsaved draft was preserved. Compare with active; discard the draft to load the current proxy list."); } await status();
}
async function status() {
  if (!session || togglePending) return;
  const generation = ++statusGeneration;
  try {
    const s=await api("status");
    if (!session || generation !== statusGeneration) return;
    injectionState(s.info.injection_enabled);
    $("readiness").textContent=s.ready ? "Proxy listeners ready" : "Proxy listeners not ready";
    $("statusLine").textContent=`Active revision ${s.info.config_revision} · Updated ${new Date().toLocaleTimeString()}`;
    $("ruleCounters").textContent=pretty(s.revision_rule_counters);
    $("outcomes").textContent=pretty(s.outcomes);
    $("counters").replaceChildren();
    for (const [key,value] of Object.entries(s.run_counters)) {
      if (typeof value === "number") { const card=element("div",undefined,$("counters")); card.className="metric"; element("strong",String(value),card); element("span",key.replaceAll("_"," "),card); }
    }
    if (active && s.info.config_revision !== active.revision) { await loadActive(); reviewed=""; showDiff(); }
  } catch (error) { if (generation !== statusGeneration) return; $("injectionBadge").textContent="Injection status unavailable"; $("injectionBadge").dataset.state="unknown"; $("statusLine").textContent="Status unavailable — displayed data may be stale."; message(error.message); }
}
function act(fn) { return async () => { try { message(); await fn(); } catch(error) { reportError(error); } }; }
$("toastClose").onclick=()=>{clearTimeout(toastTimer);$("toast").hidden=true;};
$("login").onsubmit=async event=>{event.preventDefault(); try { const fields=new FormData(event.target); session=await api("login",{name:fields.get("name"),password:fields.get("password")}); event.target.reset(); message(); await signedIn(); } catch(error) { message(error.message); }};
$("logout").onclick=act(async()=>{await api("logout",{}); session=undefined; $("injectionBadge").hidden=true; draft=undefined; $("workspace").hidden=true; $("loginPanel").hidden=false; $("logout").hidden=true; $("identity").textContent="";});
$("proxy").onchange=()=>{selected=Number($("proxy").value);renderRules();};
$("add").onclick=()=>{const p=draft.proxies[selected];p.rules.push({ID:`rule-${Date.now()}`,Enabled:true,Match:{Method:"",Path:"",PathPattern:"",Service:"",Headers:{}},Select:{Probability:1},Fault:defaultFault("delay", active.proxies.find(x => x.id === p.id)?.protocol)});edited();renderRules();};
$("refresh").onclick=act(async()=>{await loadActive();reviewed="";showDiff();message("Latest active loaded for comparison. Your draft is unchanged.");});
$("rebase").onclick=()=>{if(confirm("Keep ALL draft rules shown on the right and use the latest revision as the base? This may replace another tester's edits. Compare both panels first.")){draft.base_revision=active.revision;edited();}};
$("discard").onclick=act(async()=>{if(confirm("Discard all edits in this tab and load the active config?")){await loadActive();resetDraft();$("validation").textContent="Draft reset to active config.";}});
$("validate").onclick=act(async()=>{for(const el of $("rules").querySelectorAll("input,textarea")){if(!el.reportValidity())return;} for (const p of draft.proxies) for (const r of p.rules) { const kind=pathKinds.get(r); if(kind && kind!=="Any" && !r.Match[kind==="Pattern"?"PathPattern":"Path"])throw Error(`Path is required in rule ${r.ID}`); const raw=headerDrafts.get(r); if(raw!==undefined){let h;try{h=JSON.parse(raw);}catch{throw Error(`Invalid headers JSON in rule ${r.ID}`);}if(!h||Array.isArray(h)||typeof h!=="object"||Object.values(h).some(v=>typeof v!=="string"))throw Error(`Headers must have string values in rule ${r.ID}`);}}
  const submitted=JSON.stringify(draft); await api("validate",draft); if(submitted!==JSON.stringify(draft))throw Error("Draft changed during validation. Validate again."); reviewed=submitted; $("validation").textContent="Valid draft. Review the active and draft rules below, then apply."; showDiff();});
$("apply").onclick=act(async()=>{
  if(reviewed!==JSON.stringify(draft))return;
  busy=true;$("workspace").inert=true;showDiff();
  try {const result=await api("apply",draft);await loadActive();resetDraft();$("validation").textContent=result.changed?`Applied revision ${result.info.config_revision}.`:`No effective change; revision ${result.info.config_revision} retained.`;await status();}
  finally {busy=false;$("workspace").inert=false;showDiff();}
});
for(const op of ["enable","disable"]) $(op).onclick=act(async()=>{
  togglePending=true; ++statusGeneration;
  $("enable").disabled=true; $("disable").disabled=true;
  $("injectionBadge").textContent="Updating injection…";
  try {
    const result=await api(op,{});
    injectionState(result.info.injection_enabled);
    notify(`${result.changed ? "Confirmed" : "Already set"}: injection ${result.info.injection_enabled ? "ON" : "OFF"}.`);
    $("statusLine").textContent=`Injection confirmed by server · ${new Date().toLocaleTimeString()}`;
  } catch(error) {
    $("injectionBadge").textContent="Injection state unconfirmed";
    $("injectionBadge").dataset.state="unknown";
    throw error;
  } finally {
    togglePending=false;
    $("enable").disabled=!editor(); $("disable").disabled=!editor();
  }
});
$("auditRefresh").onclick=act(async()=>{$("audit").textContent=pretty(await api("audit"));});
window.addEventListener("beforeunload",event=>{if(draft && active && pretty(draft.proxies)!==pretty(active.proxies.map(p=>({id:p.id,rules:p.rules})))){event.preventDefault();event.returnValue="";}});
(async()=>{try{session=await api("session");await signedIn();}catch{}})();
setInterval(status,5000);
