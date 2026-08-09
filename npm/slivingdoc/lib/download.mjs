// lib/download.mjs — streamed HTTP(S) downloads with redirect handling.
//
// Binaries stream to a file so large release assets never buffer in memory;
// the SHA-256 is computed over the exact received bytes while streaming. A
// failed or interrupted download raises, and the caller removes the partial
// file — the launcher never executes unverified or partial bytes.

import { createHash } from "node:crypto";
import { createWriteStream } from "node:fs";
import { once } from "node:events";
import http from "node:http";
import https from "node:https";

const MAX_REDIRECTS = 10;
const IDLE_TIMEOUT_MS = 30_000;
const MAX_TEXT_BYTES = 1 << 20; // 1 MiB

function clientFor(protocol) {
	if (protocol === "https:") return https;
	if (protocol === "http:") return http;
	throw new Error(`unsupported download protocol: ${protocol}`);
}

function requestStream(url, redirectsLeft, method) {
	const parsed = url instanceof URL ? url : new URL(url);
	if (redirectsLeft < 0) {
		throw new Error(`too many redirects while downloading ${parsed.pathname}`);
	}
	return new Promise((resolve, reject) => {
		const req = clientFor(parsed.protocol).request(
			parsed,
			{ method, headers: { accept: "application/octet-stream" } },
			(res) => {
				const status = res.statusCode ?? 0;
				const location = res.headers.location;
				if (status >= 300 && status < 400 && location) {
					res.resume();
					requestStream(new URL(location, parsed), redirectsLeft - 1, method).then(resolve, reject);
					return;
				}
				if (status !== 200) {
					res.resume();
					reject(new Error(`download of ${parsed.pathname} failed: HTTP ${status}`));
					return;
				}
				resolve(res);
			},
		);
		req.setTimeout(IDLE_TIMEOUT_MS, () => {
			req.destroy(new Error(`download of ${parsed.pathname} timed out`));
		});
		req.on("error", reject);
		req.end();
	});
}

export async function downloadToFile(url, destPath) {
	const res = await requestStream(url, MAX_REDIRECTS, "GET");
	const hash = createHash("sha256");
	const out = createWriteStream(destPath, { flags: "wx" });
	try {
		for await (const chunk of res) {
			hash.update(chunk);
			if (!out.write(chunk)) {
				await once(out, "drain");
			}
		}
		out.end();
		await once(out, "finish");
	} catch (err) {
		out.destroy();
		throw err;
	}
	return hash.digest("hex");
}

export async function downloadText(url, limit = MAX_TEXT_BYTES) {
	const res = await requestStream(url, MAX_REDIRECTS, "GET");
	const chunks = [];
	let total = 0;
	for await (const chunk of res) {
		total += chunk.length;
		if (total > limit) {
			throw new Error(`download of ${url.pathname} exceeds ${limit} bytes`);
		}
		chunks.push(chunk);
	}
	return Buffer.concat(chunks).toString("utf8");
}

export async function headStatus(url, redirectsLeft = MAX_REDIRECTS) {
	const parsed = url instanceof URL ? url : new URL(url);
	if (redirectsLeft < 0) {
		throw new Error(`too many redirects while checking ${parsed.pathname}`);
	}
	return new Promise((resolve, reject) => {
		const req = clientFor(parsed.protocol).request(
			parsed,
			{ method: "HEAD", headers: { accept: "application/octet-stream" } },
			(res) => {
				const status = res.statusCode ?? 0;
				const location = res.headers.location;
				if (status >= 300 && status < 400 && location) {
					res.resume();
					headStatus(new URL(location, parsed), redirectsLeft - 1).then(resolve, reject);
					return;
				}
				res.resume();
				resolve(status);
			},
		);
		req.setTimeout(IDLE_TIMEOUT_MS, () => {
			req.destroy(new Error(`check of ${parsed.pathname} timed out`));
		});
		req.on("error", reject);
		req.end();
	});
}
