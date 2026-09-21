const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const path = require("node:path");

function editor(protocol) {
  class Element {
    constructor(tag, text = "") { this.tag = tag; this.textContent = text; this.children = []; this.dataset = {}; }
    append(child) { this.children.push(child); }
    replaceChildren() { this.children = []; }
    addEventListener() {}
    setAttribute() {}
  }
  const elements = new Map();
  const context = vm.createContext({
    document: {getElementById(id) { if (!elements.has(id)) elements.set(id, new Element("div")); return elements.get(id); }, createElement(tag) { return new Element(tag); }},
    window: {addEventListener() {}},
    fetch() { return new Promise(() => {}); },
    renderRuleDiff() {},
    setInterval() {}, clearTimeout() {}, setTimeout() {},
  });
  vm.runInContext(fs.readFileSync(path.join(__dirname, "ui/app.js"), "utf8"), context);
  vm.runInContext(`
    session = {role:"editor"};
    caps = {actions:{mysql:["delay","hold_response","close_connection"],postgresql:["delay","hold_response","close_connection"],http1:["delay","hold_response","close_connection","respond"]}, phases:["before_upstream_request","after_upstream_headers"], protocol_phases:{mysql:["after_commit"],postgresql:["after_commit"]}, selectors:["Probability","Nth","Every"]};
    active = {proxies:[{id:"proxy",protocol:${JSON.stringify(protocol)},listen:"local",upstream:"upstream"}]};
    draft = {proxies:[{id:"proxy",rules:[{ID:"r",Enabled:true,Match:{},Select:{Probability:1},Fault:defaultFault("delay",${JSON.stringify(protocol)})}]}]};
    renderRules();
  `, context);
  function texts(el) { return [el.textContent, ...el.children.flatMap(texts)].filter(Boolean); }
  return {context, labelsNow: () => texts(elements.get("rules")), labels: texts(elements.get("rules"))};
}

test("PostgreSQL editor exposes commit faults without HTTP matching", () => {
  const {context, labels} = editor("postgresql");
  assert(labels.includes("after_commit"));
  assert(!labels.some(text => text.startsWith("HTTP method") || text === "Path match" || text.startsWith("Headers /")));
  assert(!labels.includes("respond"));
  assert.equal(vm.runInContext('defaultFault("hold_response","postgresql").Phase', context), "after_commit");
  assert.equal(vm.runInContext('defaultFault("close_connection","postgresql").Phase', context), "after_commit");
});

test("MySQL editor exposes commit faults without HTTP matching", () => {
  const {context, labels} = editor("mysql");
  assert(labels.includes("after_commit"));
  assert(!labels.some(text => text.startsWith("HTTP method") || text === "Path match" || text.startsWith("Headers /")));
  assert(!labels.includes("respond"));
  assert.equal(vm.runInContext('defaultFault("hold_response","mysql").Phase', context), "after_commit");
  assert.equal(vm.runInContext('defaultFault("close_connection","mysql").Phase', context), "after_commit");
});

test("HTTP editor retains its own matching and phases", () => {
  const {labels} = editor("http1");
  assert(labels.includes("HTTP method (blank = any)"));
  assert(labels.includes("Path match"));
  assert(labels.includes("Headers / metadata (JSON object)"));
  assert(labels.includes("before_upstream_request"));
  assert(!labels.includes("after_commit"));
});

for (const changed of [false, true]) {
  test(`Login refreshes proxy list unless unsaved edits exist (${changed})`, async () => {
    const {context} = editor("postgresql");
    vm.runInContext(`
      active.revision = 1;
      active.proxies[0].rules = clone(draft.proxies[0].rules);
      draft.base_revision = 1;
      ${changed ? 'edited();' : ''}
      reviewed = JSON.stringify(draft);
      const newActive = {revision:2,proxies:[{id:"new-proxy",protocol:"postgresql",rules:[],listen:"local",upstream:"upstream"}]};
      api = async path => path === "config" ? newActive : caps;
      status = async () => {};
    `, context);
    await vm.runInContext('signedIn()', context);
    assert.equal(vm.runInContext('$("proxy").children[0].textContent', context), changed ? "proxy" : "new-proxy");
    assert.equal(vm.runInContext('active.revision', context), 2);
    assert.equal(vm.runInContext('draft.base_revision', context), changed ? 1 : 2);
    assert.equal(vm.runInContext('reviewed', context), "");
    if (changed) assert.match(vm.runInContext('$("message").textContent', context), /unsaved draft/);
  });
}

 test("Switching MySQL, PostgreSQL, HTTP and gRPC preserves drafts and protocol fields", () => {
   const {context, labelsNow} = editor("mysql");
   vm.runInContext(`
     caps.actions.grpc = ["delay","hold_response","truncate","throttle"];
     active.proxies = ["mysql","postgresql","http1","grpc"].map(protocol => ({id:protocol,protocol,listen:"local",upstream:"upstream"}));
     draft.proxies = active.proxies.map(p => ({id:p.id,rules:[{ID:"r",Enabled:true,Match:{},Select:{Nth:2},Fault:defaultFault("delay",p.protocol)}]}));
     draft.proxies[0].rules[0].Fault.Duration = 300000000;
   `, context);
   for (const index of [0, 1, 2, 3, 0]) {
     vm.runInContext(`selected=${index}; renderRules(); showDiff();`, context);
     const labels = labelsNow();
     assert.equal(labels.includes("after_commit"), index < 2);
     assert.equal(labels.includes("Path match"), index >= 2);
     assert.equal(labels.includes("gRPC service (optional)"), index === 3);
   }
   assert.equal(vm.runInContext("draft.proxies[0].rules[0].Fault.Duration", context), 300000000);
 });
