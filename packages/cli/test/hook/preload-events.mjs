// Loaded with --import into the processes test/hook/process.test.ts runs:
// records, in order, each line written to stdout and each attempt to open
// a socket, look up a host or start a process, to the file named by
// MEMAX_TEST_EVENTS, as `<pid> <event>`. It changes nothing else.
import childProcess from "node:child_process";
import dgram from "node:dgram";
import dns from "node:dns";
import fs from "node:fs";
import net from "node:net";
import tls from "node:tls";
import { syncBuiltinESMExports } from "node:module";

const file = process.env.MEMAX_TEST_EVENTS;
const append = fs.appendFileSync;
const log = (event) => {
  if (file) append(file, `${process.pid} ${event}\n`);
};

const writeSync = fs.writeSync;
fs.writeSync = function (fd, ...rest) {
  if (fd === 1) log("print");
  return writeSync.call(this, fd, ...rest);
};
const stdoutWrite = process.stdout.write;
process.stdout.write = function (...args) {
  log("print");
  return stdoutWrite.apply(this, args);
};

const connect = net.Socket.prototype.connect;
net.Socket.prototype.connect = function (...args) {
  log("socket");
  return connect.apply(this, args);
};
const tlsConnect = tls.connect;
tls.connect = function (...args) {
  log("socket");
  return tlsConnect.apply(this, args);
};
const createSocket = dgram.createSocket;
dgram.createSocket = function (...args) {
  log("socket");
  return createSocket.apply(this, args);
};
const lookup = dns.lookup;
dns.lookup = function (...args) {
  log("dns");
  return lookup.apply(this, args);
};
if (typeof globalThis.fetch === "function") {
  const f = globalThis.fetch;
  globalThis.fetch = function (...args) {
    log("socket");
    return f.apply(this, args);
  };
}
const spawn = childProcess.spawn;
childProcess.spawn = function (...args) {
  log(`spawn ${[args[0], ...(Array.isArray(args[1]) ? args[1] : [])].join(" ")}`);
  return spawn.apply(this, args);
};
syncBuiltinESMExports();
