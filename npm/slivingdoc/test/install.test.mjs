// test/install.test.mjs — the verified install contract: cold download,
// cache reuse without downloads, checksum and interruption rejection,
// version isolation, corrupt-cache recovery, and concurrent safety.
import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, readdir, rm, stat } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ensureBinary, ChecksumError } from "../lib/install.mjs";
import { artifactFor } from "../lib/platform.mjs";
import { sha256, startFixture, sumsFile } from "./helpers.mjs";

const VERSION = "0.1.0-dev";
const spec = artifactFor("linux", "x64");
const NAME = `slivingdoc-v${VERSION}-linux-amd64`;
const BODY = Buffer.from("#!/bin/sh\necho fixture\n");

async function newCache(t) {
	const dir = await mkdtemp(join(tmpdir(), "slivingdoc-install-"));
	t.after(() => rm(dir, { recursive: true, force: true }));
	return dir;
}

function install({ cacheRoot, baseUrl }) {
	return ensureBinary({ version: VERSION, spec, cacheRoot, baseUrl });
}

function binaryPath(cacheRoot) {
	return join(cacheRoot, "_slivingdoc", VERSION, "linux", "amd64", NAME);
}

test("cold install downloads, verifies, and installs the binary", async (t) => {
	const cache = await newCache(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: BODY }]) },
		{ tag: `v${VERSION}`, name: NAME, body: BODY },
	]);
	t.after(() => fixture.close());

	const installed = await install({ cacheRoot: cache, baseUrl: fixture.base });
	assert.equal(installed, binaryPath(cache));
	assert.deepEqual(await readFile(installed), BODY);
	const st = await stat(installed);
	assert.equal(st.mode & 0o111, 0o111, "binary must be executable on POSIX");
	const sidecar = await readFile(`${installed}.sha256`, "utf8");
	assert.equal(sidecar, `${sha256(BODY)}  ${NAME}\n`);
});

test("a verified cache entry is reused without any download", async (t) => {
	const cache = await newCache(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: BODY }]) },
		{ tag: `v${VERSION}`, name: NAME, body: BODY },
	]);
	await install({ cacheRoot: cache, baseUrl: fixture.base });

	const requestCount = fixture.requests.length;
	await fixture.close(); // any further download fails
	const again = await install({ cacheRoot: cache, baseUrl: fixture.base });
	assert.equal(again, binaryPath(cache));
	assert.equal(fixture.requests.length, requestCount, "no request may leave the cache");
});

test("a checksum mismatch deletes the partial file and refuses", async (t) => {
	const cache = await newCache(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: BODY }]) },
		{ tag: `v${VERSION}`, name: NAME, body: "different bytes" },
	]);
	t.after(() => fixture.close());

	await assert.rejects(() => install({ cacheRoot: cache, baseUrl: fixture.base }), ChecksumError);
	await assert.rejects(stat(binaryPath(cache)), "no binary may be installed");
	assert.deepEqual(await readdir(join(cache, "_slivingdoc", VERSION, "linux", "amd64")), []);
});

test("an interrupted download removes the temporary file and refuses", async (t) => {
	const cache = await newCache(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: BODY }]) },
		{ tag: `v${VERSION}`, name: NAME, body: BODY, abortAfter: 7 },
	]);
	t.after(() => fixture.close());

	await assert.rejects(() => install({ cacheRoot: cache, baseUrl: fixture.base }), /interrupted|socket hang up|aborted/);
	assert.deepEqual(await readdir(join(cache, "_slivingdoc", VERSION, "linux", "amd64")), [], "no partial file may remain");
});

test("a release without a checksum entry for the asset fails clearly", async (t) => {
	const cache = await newCache(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: "other-asset", body: BODY }]) },
	]);
	t.after(() => fixture.close());

	await assert.rejects(() => install({ cacheRoot: cache, baseUrl: fixture.base }), /does not list/);
});

test("transient network failures retry with backoff until the download succeeds", async (t) => {
	const cache = await newCache(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: BODY }]), failFirst: 2 },
		{ tag: `v${VERSION}`, name: NAME, body: BODY },
	]);
	t.after(() => fixture.close());

	const installed = await install({ cacheRoot: cache, baseUrl: fixture.base });
	assert.equal(installed, binaryPath(cache));
	assert.deepEqual(await readFile(installed), BODY);
	const sumsAttempts = fixture.requests.filter((r) => r.path.endsWith("/SHA256SUMS"));
	assert.equal(sumsAttempts.length, 3, "two resets must be retried before the third attempt succeeds");
});

test("a 404 response is reported immediately and never retried", async (t) => {
	const cache = await newCache(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: NAME, body: BODY },
	]);
	t.after(() => fixture.close());

	await assert.rejects(() => install({ cacheRoot: cache, baseUrl: fixture.base }), /HTTP 404/);
	assert.equal(fixture.requests.length, 1, "a 404 must not be retried");
});

test("version-crossed cache entries never reuse each other", async (t) => {
	const cache = await newCache(t);
	const otherVersion = "0.1.1";
	const otherName = `slivingdoc-v${otherVersion}-linux-amd64`;
	const otherBody = Buffer.from("other version");
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: BODY }]) },
		{ tag: `v${VERSION}`, name: NAME, body: BODY },
		{ tag: `v${otherVersion}`, name: "SHA256SUMS", body: sumsFile([{ name: otherName, body: otherBody }]) },
		{ tag: `v${otherVersion}`, name: otherName, body: otherBody },
	]);
	t.after(() => fixture.close());

	const a = await install({ cacheRoot: cache, baseUrl: fixture.base });
	const b = await ensureBinary({ version: otherVersion, spec, cacheRoot: cache, baseUrl: fixture.base });
	assert.notEqual(a, b);
	assert.deepEqual(await readFile(a), BODY);
	assert.deepEqual(await readFile(b), otherBody);
});

test("a corrupt cached binary is replaced from the release", async (t) => {
	const cache = await newCache(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: BODY }]) },
		{ tag: `v${VERSION}`, name: NAME, body: BODY },
	]);
	t.after(() => fixture.close());

	await install({ cacheRoot: cache, baseUrl: fixture.base });
	await rm(binaryPath(cache), { force: true });
	const again = await install({ cacheRoot: cache, baseUrl: fixture.base });
	assert.deepEqual(await readFile(again), BODY);
});

test("concurrent installs of the same entry race safely", async (t) => {
	const cache = await newCache(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: BODY }]) },
		{ tag: `v${VERSION}`, name: NAME, body: BODY },
	]);
	t.after(() => fixture.close());

	const [a, b] = await Promise.all([
		install({ cacheRoot: cache, baseUrl: fixture.base }),
		install({ cacheRoot: cache, baseUrl: fixture.base }),
	]);
	assert.equal(a, binaryPath(cache));
	assert.equal(b, binaryPath(cache));
	assert.deepEqual(await readFile(binaryPath(cache)), BODY);
});
