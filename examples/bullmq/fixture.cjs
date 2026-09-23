"use strict";
// Adds one job through Faultline, optionally retries on a fresh connection,
// then observes Redis directly. Prints counts and error classes, never payloads.
const fs = require("node:fs");
const {Queue} = require("bullmq");
const Redis = require("ioredis");

const env = process.env;
const secure = env.TLS_DIR ? {ca: fs.readFileSync(`${env.TLS_DIR}/cert.pem`), servername: env.TLS_SERVERNAME || "localhost"} : undefined;
const auth = env.REDIS_AUTH ? {username: "fixture", password: "fixture-only-password"} : {};
const queueName = env.QUEUE || "fixture";
const jobOptions = env.JOB_ID ? {jobId: env.JOB_ID} : {};

// Fixed caller policy from the P28 contract: no implicit resend or reconnect.
const connect = port => {
  const client = new Redis({host: "127.0.0.1", port, ...auth, tls: secure, connectTimeout: 2000, commandTimeout: 2000,
    maxRetriesPerRequest: 0, autoResendUnfulfilledCommands: false, retryStrategy: () => null});
  client.on("error", () => {});
  return client;
};

async function attempt(port) {
  const client = connect(port), queue = new Queue(queueName, {connection: client});
  const started = Date.now();
  try {
    await queue.add("fixture", {fixture: true}, jobOptions);
    return {ok: true, ms: Date.now() - started};
  } catch (error) {
    return {ok: false, ms: Date.now() - started, error: error.name};
  } finally {
    await queue.close().catch(() => {});
    client.disconnect();
  }
}

(async () => {
  const timer = setTimeout(() => { console.error("fixture exceeded 15s"); process.exit(2); }, 15000);
  const proxyPort = Number(env.PROXY_PORT), redisPort = Number(env.REDIS_PORT);
  const first = await attempt(proxyPort);
  const retry = env.RETRY ? await attempt(proxyPort) : undefined;
  const observer = connect(redisPort);
  const waiting = await observer.llen(`bull:${queueName}:wait`);
  const known = env.JOB_ID ? await observer.exists(`bull:${queueName}:${env.JOB_ID}`) : undefined;
  observer.disconnect();
  clearTimeout(timer);
  console.log(JSON.stringify({first, retry, waiting, known}));
})().catch(error => { console.error("fixture failed:", error.name); process.exit(1); });
