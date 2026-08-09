// test/helpers.mjs — fixture HTTP server and process runner for the
// launcher suite. The fixture serves a GitHub-release-shaped tree at
// /download/<tag>/<asset> so the launcher and the publication gate can run
// against deterministic local bytes instead of the network.

import { createHash } from "node:crypto";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { createServer } from "node:http";

// startFixture serves the given assets. Each entry is
// { tag, name, body, abortAfter? } where abortAfter truncates the body and
// destroys the connection (an interrupted download).
export async function startFixture(assets) {
	const routes = new Map();
	const requests = [];
	for (const asset of assets) {
		routes.set(`/download/${asset.tag}/${asset.name}`, asset);
	}
	const server = createServer((req, res) => {
		const path = new URL(req.url, "http://localhost").pathname;
		requests.push({ method: req.method, path });
		const asset = routes.get(path);
		if (!asset) {
			res.writeHead(404).end("not found");
			return;
		}
		const body = Buffer.isBuffer(asset.body) ? asset.body : Buffer.from(asset.body);
		res.writeHead(200, { "content-length": String(body.length) });
		if (req.method === "HEAD") {
			res.end();
			return;
		}
		if (asset.abortAfter !== undefined) {
			res.write(body.subarray(0, asset.abortAfter));
			res.destroy(new Error("fixture: interrupted download"));
			return;
		}
		res.end(body);
	});
	server.listen(0, "127.0.0.1");
	await once(server, "listening");
	const { port } = server.address();
	return {
		base: `http://127.0.0.1:${port}/download`,
		requests,
		close: () => new Promise((resolve) => server.close(resolve)),
	};
}

export function sha256(data) {
	return createHash("sha256").update(data).digest("hex");
}

// sumsFile emits the architecture section 21 SHA256SUMS grammar: sorted LF
// lines, lowercase digest, two spaces, asset name.
export function sumsFile(entries) {
	return entries
		.map((e) => `${sha256(e.body)}  ${e.name}`)
		.sort()
		.join("\n")
		.concat("\n");
}

// runNode runs the launcher or a helper script as a child process and
// resolves { code, signal, stdout, stderr }.
export function runNode(scriptPath, args, { env, input, timeoutMs = 30_000 } = {}) {
	return new Promise((resolve, reject) => {
		const child = spawn(process.execPath, [scriptPath, ...args], {
			env: { ...process.env, ...env },
			stdio: ["pipe", "pipe", "pipe"],
		});
		const stdout = [];
		const stderr = [];
		child.stdout.on("data", (d) => stdout.push(d));
		child.stderr.on("data", (d) => stderr.push(d));
		if (input !== undefined) {
			child.stdin.end(input);
		} else {
			child.stdin.end();
		}
		const timer = setTimeout(() => {
			child.kill("SIGKILL");
			reject(new Error(`child timed out: ${scriptPath}`));
		}, timeoutMs);
		child.on("error", (err) => {
			clearTimeout(timer);
			reject(err);
		});
		child.on("exit", (code, signal) => {
			clearTimeout(timer);
			resolve({
				code,
				signal,
				stdout: Buffer.concat(stdout).toString("utf8"),
				stderr: Buffer.concat(stderr).toString("utf8"),
			});
		});
	});
}
