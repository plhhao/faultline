"use strict";

function diffFields(value, path = [], result = new Map()) {
  if (value == null && path.length === 2 && path[0] === "Fault" &&
      ["Duration", "MaxDuration", "Status", "Body", "Bytes", "BytesPerSecond"].includes(path[1])) return result;
  if (value !== null && typeof value === "object" && !Array.isArray(value) && Object.keys(value).length) {
    for (const key of Object.keys(value).sort()) diffFields(value[key], [...path, key], result);
  } else {
    result.set(JSON.stringify(path), JSON.stringify(value));
  }
  return result;
}

function ruleDiffRows(before, after) {
  const left = diffFields(before), right = diffFields(after);
  return [...new Set([...left.keys(), ...right.keys()])].map(key => ({
    path: JSON.parse(key).map(part => JSON.stringify(part)).join(" → "),
    before: left.get(key), after: right.get(key),
    kind: !left.has(key) ? "added" : !right.has(key) ? "removed" : left.get(key) !== right.get(key) ? "changed" : "same"
  }));
}

function pairRules(before, after) {
  const unique = (rules, id) => rules.filter(rule => rule.ID === id).length === 1;
  const used = new Set();
  const pairs = before.map((rule, index) => {
    const next = unique(before, rule.ID) && unique(after, rule.ID) ? after.findIndex(r => r.ID === rule.ID) : -1;
    if (next >= 0) used.add(next);
    return {before: rule, after: next >= 0 ? after[next] : undefined, oldIndex: index, newIndex: next};
  });
  after.forEach((rule, index) => { if (!used.has(index)) pairs.push({after:rule, oldIndex:-1, newIndex:index}); });
  return pairs;
}

function renderRuleDiff(left, right, before, after) {
  left.replaceChildren(); right.replaceChildren();
  const add = (parent, tag, text, className = "") => {
    const node = document.createElement(tag);
    node.textContent = text; node.className = className; parent.append(node);
    return node;
  };
  for (const id of new Set([...before.map(p => p.id), ...after.map(p => p.id)])) {
    const oldRules = before.find(p => p.id === id)?.rules || [];
    const newRules = after.find(p => p.id === id)?.rules || [];
    add(left, "h4", `Proxy: ${id}`); add(right, "h4", `Proxy: ${id}`);
    const pairs = pairRules(oldRules, newRules);
    if (!pairs.length) { add(left, "p", "No rules"); add(right, "p", "No rules"); }
    if ([oldRules, newRules].some(rules => new Set(rules.map(r => r.ID)).size !== rules.length)) {
      for (const side of [left, right]) add(side, "p", "Duplicate rule IDs: ambiguous rules shown separately. Fix IDs before applying.", "diff-changed");
    }
    for (const pair of pairs) {
      const a = add(left, "section", "", "diff-rule"), b = add(right, "section", "", "diff-rule");
      const title = (pair.before || pair.after).ID;
      add(a, "h5", title); add(b, "h5", title);
      if (!pair.before || !pair.after) {
        const present = pair.before ? a : b, missing = pair.before ? b : a;
        add(present, "p", pair.before ? "− Removed rule" : "+ Added rule", pair.before ? "diff-removed" : "diff-added");
        add(present, "pre", JSON.stringify(pair.before || pair.after, null, 2), pair.before ? "diff-removed" : "diff-added");
        add(missing, "p", "— Rule absent", "muted");
        continue;
      }
      const moved = pair.oldIndex !== pair.newIndex;
      add(a, "p", `Position ${pair.oldIndex + 1}`, moved ? "diff-changed" : "muted");
      add(b, "p", moved ? `~ Position ${pair.oldIndex + 1} → ${pair.newIndex + 1}` : `Position ${pair.newIndex + 1}`, moved ? "diff-changed" : "muted");
      for (const row of ruleDiffRows(pair.before, pair.after)) {
        const marker = {added:"+",removed:"−",changed:"~",same:""}[row.kind];
        for (const [parent, value] of [[a,row.before],[b,row.after]]) {
          add(parent, "div", `${marker} ${row.path}: ${value === undefined ? "(absent)" : value}`, `diff-field diff-${row.kind}`);
        }
      }
    }
  }
}
