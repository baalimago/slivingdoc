// test/launcher.test.mjs — the launcher as a black box: download and
// execution, cache reuse, stream forwarding, exit and signal propagation,
// and the refusal paths (checksum mismatch and interrupted download).
import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { version } from "../lib/launcher.mjs";
import { runNode, startFixture, sumsFile } from "./helpers.mjs";

const ROOT = fileURLToPath(new URL("..", import.meta.url));
const LAUNCHER = join(ROOT, "bin", "slivingdoc.mjs");
const VERSION = version();
const NAME = `slivingdoc-v${VERSION}-linux-amd64`;

// The fixture "binary" is a POSIX shell script; its stdout and exit behavior
// let the tests assert exact stream and status forwarding.
const FIXTURE_BODY = `#!/bin/sh
printf 'argv:'
for a in "$@"; do printf ' [%s]' "$a"; done
printf '\\n'
if [ -n "\${FIXTURE_SIGNAL:-}" ]; then
	kill -"\$FIXTURE_SIGNAL" \$\$
	exit 0
fi
if [ -n "\${FIXTURE_EXIT:-}" ]; then
	exit "\$FIXTURE_EXIT"
fi
IFS= read -r line
printf 'stdin:%s\\n' "\$line"
exit 0
`;

async function newEnv(t) {
	const cache = await mkdtemp(join(tmpdir(), "slivingdoc-launcher-"));
	t.after(() => rm(cache, { recursive: true, force: true }));
	return cache;
}

async function serveRelease(assets) {
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: FIXTURE_BODY }]) },
		...assets,
	]);
	return fixture;
}

function envFor(cache, base) {
	return { SLIVINGDOC_CACHE: cache, SLIVINGDOC_RELEASE_BASE: base };
}

test("first run downloads the release and executes with exact stdout", async (t) => {
	const cache = await newEnv(t);
	const fixture = await serveRelease([{ tag: `v${VERSION}`, name: NAME, body: FIXTURE_BODY }]);
	t.after(() => fixture.close());

	const res = await runNode(LAUNCHER, ["--version"], { env: envFor(cache, fixture.base) });
	assert.equal(res.code, 0);
	assert.equal(res.signal, null);
	assert.equal(res.stdout, "argv: [--version]\nstdin:\n", "stdout must be exactly the child output");
	assert.equal(res.stderr, "", "the wrapper must not add stderr noise");
});

test("stdin stays connected and arguments are forwarded verbatim", async (t) => {
	const cache = await newEnv(t);
	const fixture = await serveRelease([{ tag: `v${VERSION}`, name: NAME, body: FIXTURE_BODY }]);
	t.after(() => fixture.close());

	const res = await runNode(LAUNCHER, ["--bucket", "my notes", "-x"], {
		env: envFor(cache, fixture.base),
		input: "hello from the host\n",
	});
	assert.equal(res.code, 0);
	assert.equal(res.stdout, "argv: [--bucket] [my notes] [-x]\nstdin:hello from the host\n");
});

test("a cached run executes without contacting the release", async (t) => {
	const cache = await newEnv(t);
	const fixture = await serveRelease([{ tag: `v${VERSION}`, name: NAME, body: FIXTURE_BODY }]);
	await runNode(LAUNCHER, [], { env: envFor(cache, fixture.base) });

	const requestCount = fixture.requests.length;
	await fixture.close();
	const res = await runNode(LAUNCHER, [], { env: envFor(cache, fixture.base) });
	assert.equal(res.code, 0);
	assert.equal(fixture.requests.length, requestCount, "a verified cache entry must not re-download");
	assert.equal(res.stdout, "argv:\nstdin:\n");
});

test("a nonzero child exit code propagates", async (t) => {
	const cache = await newEnv(t);
	const fixture = await serveRelease([{ tag: `v${VERSION}`, name: NAME, body: FIXTURE_BODY }]);
	t.after(() => fixture.close());

	const res = await runNode(LAUNCHER, [], {
		env: { ...envFor(cache, fixture.base), FIXTURE_EXIT: "5" },
	});
	assert.equal(res.code, 5, "the wrapper must exit with the child's exit code");
});

test("a signal-killed child kills the wrapper by the same signal", { skip: process.platform === "win32" }, async (t) => {
	const cache = await newEnv(t);
	const fixture = await serveRelease([{ tag: `v${VERSION}`, name: NAME, body: FIXTURE_BODY }]);
	t.after(() => fixture.close());

	const res = await runNode(LAUNCHER, [], {
		env: { ...envFor(cache, fixture.base), FIXTURE_SIGNAL: "TERM" },
	});
	assert.equal(res.signal, "SIGTERM", "a signal death must be re-raised by the wrapper");
});

test("a checksum mismatch refuses execution and leaves no cache entry", async (t) => {
	const cache = await newEnv(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: FIXTURE_BODY }]) },
		{ tag: `v${VERSION}`, name: NAME, body: "not the fixture" },
	]);
	t.after(() => fixture.close());

	const res = await runNode(LAUNCHER, [], { env: envFor(cache, fixture.base) });
	assert.notEqual(res.code, 0);
	assert.equal(res.stdout, "", "nothing may execute");
	assert.match(res.stderr, /checksum mismatch/);
	assert.deepEqual(await readdir(join(cache, "_slivingdoc", VERSION, "linux", "amd64")), []);
});

test("an interrupted download refuses execution and leaves no partial file", async (t) => {
	const cache = await newEnv(t);
	const fixture = await startFixture([
		{ tag: `v${VERSION}`, name: "SHA256SUMS", body: sumsFile([{ name: NAME, body: FIXTURE_BODY }]) },
		{ tag: `v${VERSION}`, name: NAME, body: FIXTURE_BODY, abortAfter: 9 },
	]);
	t.after(() => fixture.close());

	const res = await runNode(LAUNCHER, [], { env: envFor(cache, fixture.base) });
	assert.notEqual(res.code, 0);
	assert.equal(res.stdout, "", "nothing may execute");
	assert.deepEqual(await readdir(join(cache, "_slivingdoc", VERSION, "linux", "amd64")), []);
});

test("a release base with a trailing slash still resolves", async (t) => {
	const cache = await newEnv(t);
	const fixture = await serveRelease([{ tag: `v${VERSION}`, name: NAME, body: FIXTURE_BODY }]);
	t.after(() => fixture.close());

	const res = await runNode(LAUNCHER, [], {
		env: { SLIVINGDOC_CACHE: cache, SLIVINGDOC_RELEASE_BASE: `${fixture.base}/` },
	});
	assert.equal(res.code, 0, res.stderr);
	assert.equal(res.stdout, "argv:\nstdin:\n");
});

test("a release missing the checksum file fails with a clear error", async (t) => {
	const cache = await newEnv(t);
	const fixture = await startFixture([{ tag: `v${VERSION}`, name: NAME, body: FIXTURE_BODY }]);
	t.after(() => fixture.close());

	const res = await runNode(LAUNCHER, [], { env: envFor(cache, fixture.base) });
	assert.notEqual(res.code, 0);
	assert.equal(res.stdout, "");
	assert.match(res.stderr, /SHA256SUMS/);
});
