"use strict";
const net = require("node:net");
const tls = require("node:tls");
const fs = require("node:fs");
const crypto = require("node:crypto");
const assert = require("node:assert/strict");
const {Queue} = require("bullmq");
const Redis = require("ioredis");
const {addStandardJob} = require("bullmq/dist/cjs/scripts/addStandardJob-9");

// A fixture-only wire observer. Emit command names and shapes, never arguments.
function parse(b, start = 0) {
  const end = b.indexOf("\r\n", start);
  if (end < 0) return;
  const kind = String.fromCharCode(b[start]), line = b.subarray(start + 1, end);
  let next = end + 2;
  if (kind === "$") {
    const size = Number(line);
    if (size === -1) return {kind, value: null, next};
    if (b.length < next + size + 2) return;
    return {kind, value: b.subarray(next, next + size), next: next + size + 2};
  }
  if (kind === "*") {
    const value = [];
    for (let i = 0; i < Number(line); i++) {
      const item = parse(b, next);
      if (!item) return;
      value.push(item); next = item.next;
    }
    return {kind, value, next};
  }
  return {kind, value: line, next};
}
function observe(socket, callback) {
  let buffer = Buffer.alloc(0);
  socket.on("data", data => {
    buffer = Buffer.concat([buffer, data]);
    for (;;) {
      const value = parse(buffer);
      if (!value) break;
      callback(value); buffer = buffer.subarray(value.next);
    }
  });
}

(async () => {
  const timer = setTimeout(() => process.exit(2), 15000);
  const port = Number(process.env.REDIS_PORT || 16379);
  const secure = process.env.TLS_DIR;
  const tlsOptions = secure ? {ca: fs.readFileSync(`${secure}/cert.pem`), servername: "localhost"} : undefined;
  const auth = process.env.REDIS_AUTH ? {username: "fixture", password: "fixture-only-password"} : {};
  const observer = new Redis({port, ...auth, tls: tlsOptions, maxRetriesPerRequest: 0, retryStrategy: () => null});
  const stale = await observer.keys("bull:contract:*"); if (stale.length) await observer.del(...stale);
  const rows = [], sockets = new Set();
  let serial = 0, stage = "handshake";
  const handle = client => {
    const connection = ++serial, pending = [];
    const upstream = secure ? tls.connect({port, host: "127.0.0.1", ...tlsOptions}) : net.connect(port, "127.0.0.1");
    for (const socket of [client, upstream]) {
      sockets.add(socket); socket.on("close", () => sockets.delete(socket));
      socket.on("error", () => {client.destroy(); upstream.destroy();});
    }
    client.on("close", () => upstream.destroy());
    upstream.on("close", () => client.destroy());
    observe(client, frame => {
      const args = frame.value.map(x => x.value), command = args[0].toString().toUpperCase();
      const row = {stage, connection, command, argc: args.length};
      if (command === "EVAL" || command === "EVALSHA") {
        row.sha = command === "EVAL" ? crypto.createHash("sha1").update(args[1]).digest("hex") : args[1].toString();
        row.keys = Number(args[2]);
      }
      rows.push(row); pending.push(row);
    });
    observe(upstream, frame => {
      const row = pending.shift(); assert.ok(row);
      row.reply = frame.kind;
      row.length = Buffer.isBuffer(frame.value) ? frame.value.length : undefined;
      if (frame.kind === "-") row.error = frame.value.toString().split(" ")[0];
    });
    client.pipe(upstream); upstream.pipe(client);
  };
  const relay = secure ? tls.createServer({cert: fs.readFileSync(`${secure}/cert.pem`), key: fs.readFileSync(`${secure}/key.pem`)}, handle) : net.createServer(handle);
  await new Promise(resolve => relay.listen(0, "127.0.0.1", resolve));
  const client = new Redis({port: relay.address().port, ...auth, tls: tlsOptions, maxRetriesPerRequest: 0,
    retryStrategy: () => null, autoResendUnfulfilledCommands: false, commandTimeout: 2000});
  client.on("error", () => {});
  const queue = new Queue("contract", {connection: client});
  await queue.waitUntilReady();
  await observer.script("FLUSH");
  stage = "cold"; const first = await queue.add("fixture", {}, {jobId: "fixed"});
  stage = "warm"; await queue.add("fixture", {}, {jobId: "second"});
  stage = "duplicate"; const duplicate = await queue.add("fixture", {}, {jobId: "fixed"});
  assert.equal(first.id, duplicate.id);
  await observer.script("FLUSH");
  stage = "cache-miss"; await queue.add("fixture", {});
  stage = "concurrent"; await Promise.all([queue.add("fixture", {}), queue.add("fixture", {})]);
  stage = "semantic-error";
  await assert.rejects(queue.add("fixture", {}, {parent: {id: "missing", queue: "bull:parent"}}));
  stage = "duplicate-connection";
  const duplicateClient = client.duplicate(); await duplicateClient.ping();
  stage = "blocking"; assert.equal(await duplicateClient.blpop("bull:contract:empty", 0.1), null);
  stage = "reconnect";
  await new Promise(resolve => {duplicateClient.once("end", resolve); duplicateClient.disconnect();});
  await duplicateClient.connect(); await duplicateClient.ping();
  await duplicateClient.quit();
  assert.equal(await observer.llen("bull:contract:wait"), 5);
  await queue.close(); await client.quit();
  const keys = await observer.keys("bull:contract:*"); if (keys.length) await observer.del(...keys);
  await observer.quit();
  for (const socket of sockets) socket.destroy();
  await new Promise(resolve => relay.close(resolve)); clearTimeout(timer);
  console.log(JSON.stringify({node: process.version, bullmq: require("bullmq/package.json").version,
    ioredis: require("ioredis/package.json").version,
    scriptSHA: crypto.createHash("sha1").update(addStandardJob.content).digest("hex"), rows}, null, 2));
})().catch(error => {console.error("BullMQ trace failed", error.name, error.code || "", error.stack?.split("\n").filter(line => line.trim().startsWith("at " )).join("\n")); process.exit(1);});
