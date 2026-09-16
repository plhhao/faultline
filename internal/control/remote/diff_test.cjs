const {test} = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const vm = require("node:vm");
const ctx = vm.createContext({});
vm.runInContext(fs.readFileSync(`${__dirname}/ui/diff.js`, "utf8"), ctx);
const rule = (ID, probability = 1) => ({ID, Select:{Probability:probability}});

test("pairs rules by ID, reports positions without marking fields changed", () => {
  const pairs = ctx.pairRules([rule("a"),rule("b")],[rule("b"),rule("a")]);
  assert.equal(pairs[0].newIndex,1);
  assert.equal(pairs[1].newIndex,0);
  assert.ok(ctx.ruleDiffRows(pairs[0].before,pairs[0].after).every(r=>r.kind==="same"));
});
test("single field change and nested header additions/removals", () => {
  const rows = ctx.ruleDiffRows({Select:{Probability:0},Match:{Headers:{old:"x"}}},{Select:{Probability:0.5},Match:{Headers:{new:"y"}}});
  assert.equal(rows.filter(r=>r.kind==="changed").length,1);
  assert.equal(rows.filter(r=>r.kind==="added").length,1);
  assert.equal(rows.filter(r=>r.kind==="removed").length,1);
});
test("rename is removal/addition, duplicates are not silently paired", () => {
  const pairs = ctx.pairRules([rule("a")],[rule("b")]);
  assert.equal(pairs.length,2);
  assert.equal(pairs[0].after,undefined);
  assert.equal(pairs[1].before,undefined);
  assert.equal(ctx.pairRules([rule("a")],[rule("a"),rule("a")]).length,3);
});
test("object key order is ignored; null, empty and dotted header keys remain distinct", () => {
  assert.ok(ctx.ruleDiffRows({a:1,b:2},{b:2,a:1}).every(r=>r.kind==="same"));
  assert.equal(ctx.ruleDiffRows({a:null},{a:{}})[0].kind,"changed");
  assert.equal(ctx.diffFields({"a.b":"x",a:{b:"y"}}).size,2);
});

test("unused optional fault fields treat null and absent alike without hiding actual values", () => {
  for (const field of ["Duration", "MaxDuration", "Status", "Body", "Bytes", "BytesPerSecond"]) {
    const empty = {Fault:{Action:"delay"}};
    const nullable = {Fault:{Action:"delay",[field]:null}};
    for (const [before,after] of [[empty,nullable],[nullable,empty]]) {
      assert.ok(ctx.ruleDiffRows(before,after).every(r=>r.kind==="same"));
    }
    const value = {Fault:{Action:"delay",[field]:field === "Body" ? "" : 0}};
    assert.equal(ctx.ruleDiffRows(nullable,value).filter(r=>r.kind==="added").length,1);
    assert.equal(ctx.ruleDiffRows(value,nullable).filter(r=>r.kind==="removed").length,1);
    assert.equal(nullable.Fault[field],null);
  }
});

test("rendering keeps proxy scopes separate and treats HTML as text", () => {
  class Node {
    constructor(){this.children=[];this.textContent="";}
    append(node){this.children.push(node);}
    replaceChildren(){this.children=[];}
    set innerHTML(value){throw Error("unsafe HTML rendering");}
  }
  ctx.document={createElement:()=>new Node()};
  const left=new Node(),right=new Node();
  const payload="<img src=x onerror=alert(1)>";
  ctx.renderRuleDiff(left,right,[{id:"a",rules:[rule("shared",0)]},{id:"b",rules:[rule("shared",1)]}],[{id:"a",rules:[rule("shared",0.5)]},{id:"b",rules:[{...rule("shared",1),Body:payload}]}]);
  const all = node => [node,...node.children.flatMap(all)];
  assert.equal(all(left).filter(n=>n.textContent==='Proxy: a').length,1);
  assert.equal(all(right).filter(n=>n.className==='diff-field diff-changed').length,1);
  assert.ok(all(right).some(n=>n.textContent.includes(payload)));
});
