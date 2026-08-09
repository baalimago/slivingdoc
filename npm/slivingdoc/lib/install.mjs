// lib/install.mjs — verified, atomic, concurrent-safe binary install.
//
// The cache entry lives under the cache root at
// _slivingdoc/<version>/<os>/<arch>/<asset>, so different versions, target
// platforms, and assets never collide. A cached binary is trusted only when
// its recorded checksum still matches; anything else is deleted and fetched
// again. Downloads stream into a unique temporary file and are atomically
// renamed into place, so concurrent verified downloads race safely and an
// interrupted download leaves no partial entry behind.

import { createHash, randomUUID } from "node:crypto";
import { createReadStream } from "node:fs";
import { chmod, mkdir, readFile, rename, rm, stat, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { downloadText, downloadToFile } from "./download.mjs";
import { assetName } from "./platform.mjs";
import { parseSums } from "./sums.mjs";

export class ChecksumError extends Error {
	constructor(message) {
		super(message);
		this.name = "ChecksumError";
	}
}

async function sha256File(path) {
	const hash = createHash("sha256");
	for await (const chunk of createReadStream(path)) {
		hash.update(chunk);
	}
	return hash.digest("hex");
}

async function recordedChecksum(sidecar) {
	try {
		const text = await readFile(sidecar, "utf8");
		const match = /^([0-9a-f]{64})  \S+\n$/.exec(text);
		return match ? match[1] : null;
	} catch {
		return null;
	}
}

async function verified(binary, sidecar) {
	const expected = await recordedChecksum(sidecar);
	if (!expected) {
		return false;
	}
	try {
		const st = await stat(binary);
		if (!st.isFile()) {
			return false;
		}
	} catch {
		return false;
	}
	return (await sha256File(binary)) === expected;
}

export async function ensureBinary({ version, spec, cacheRoot, baseUrl }) {
	const entryDir = join(cacheRoot, "_slivingdoc", version, spec.os, spec.arch);
	const binary = join(entryDir, assetName(version, spec));
	const sidecar = `${binary}.sha256`;
	const tag = `v${version}`;
	const name = assetName(version, spec);

	if (await verified(binary, sidecar)) {
		return binary;
	}

	// The cache entry is missing or no longer matches its recorded checksum.
	// A corrupt entry is removed before anything is downloaded again.
	await rm(binary, { force: true });
	await rm(sidecar, { force: true });

	const sums = parseSums(await downloadText(`${baseUrl}/${tag}/SHA256SUMS`));
	const expected = sums.get(name);
	if (!expected) {
		throw new Error(`release ${tag} does not list ${name}`);
	}

	await mkdir(entryDir, { recursive: true });
	const tmp = join(entryDir, `.${name}.${randomUUID()}.part`);
	try {
		const got = await downloadToFile(`${baseUrl}/${tag}/${name}`, tmp);
		if (got !== expected) {
			throw new ChecksumError(`checksum mismatch for ${name}: expected ${expected}, got ${got}`);
		}
		if (spec.os !== "windows") {
			await chmod(tmp, 0o755);
		}
		await rename(tmp, binary); // atomic; concurrent winners install the same verified bytes
		await writeFile(sidecar, `${expected}  ${name}\n`);
		return binary;
	} finally {
		await rm(tmp, { force: true });
	}
}
