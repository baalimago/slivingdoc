// test/release.test.mjs — the npm publication gate: a complete release
// passes, and any missing asset, a missing checksum file, or a version whose
// release tag does not exist blocks publication.
import test from "node:test";
import assert from "node:assert/strict";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { version } from "../lib/launcher.mjs";
import { SUPPORTED_TARGETS } from "../lib/platform.mjs";
import { runNode, startFixture, sumsFile } from "./helpers.mjs";

const ROOT = fileURLToPath(new URL("..", import.meta.url));
const CHECK = join(ROOT, "scripts", "check-release.mjs");
const VERSION = version();
const TAG = `v${VERSION}`;

function assetsFor(tag) {
	return SUPPORTED_TARGETS.map(({ os, arch }) => ({
		tag,
		name: `slivingdoc-v${VERSION}-${os}-${arch}${os === "windows" ? ".exe" : ""}`,
		body: `${os}-${arch} binary`,
	}));
}

async function completeFixture(t, extra = []) {
	const binaries = assetsFor(TAG);
	const fixture = await startFixture([
		...binaries,
		{ tag: TAG, name: "SHA256SUMS", body: sumsFile(binaries.map((b) => ({ name: b.name, body: b.body }))) },
		{ tag: TAG, name: "NOTICE", body: "third-party notices" },
		...extra,
	]);
	t.after(() => fixture.close());
	return fixture;
}

test("a complete release passes the publication gate", async (t) => {
	const fixture = await completeFixture(t);
	const res = await runNode(CHECK, [], { env: { SLIVINGDOC_RELEASE_BASE: fixture.base } });
	assert.equal(res.code, 0, res.stderr);
	assert.match(res.stdout, /contains all/);
});

test("a missing target asset blocks publication", async (t) => {
	const binaries = assetsFor(TAG).filter((b) => !b.name.includes("linux-arm64"));
	const fixture = await startFixture([
		...binaries,
		{ tag: TAG, name: "SHA256SUMS", body: sumsFile(binaries.map((b) => ({ name: b.name, body: b.body }))) },
		{ tag: TAG, name: "NOTICE", body: "third-party notices" },
	]);
	t.after(() => fixture.close());

	const res = await runNode(CHECK, [], { env: { SLIVINGDOC_RELEASE_BASE: fixture.base } });
	assert.notEqual(res.code, 0);
	assert.match(res.stderr, /linux-arm64/);
	assert.match(res.stderr, /must not precede/);
});

test("a release missing SHA256SUMS blocks publication", async (t) => {
	const binaries = assetsFor(TAG);
	const fixture = await startFixture([
		...binaries,
		{ tag: TAG, name: "NOTICE", body: "third-party notices" },
	]);
	t.after(() => fixture.close());

	const res = await runNode(CHECK, [], { env: { SLIVINGDOC_RELEASE_BASE: fixture.base } });
	assert.notEqual(res.code, 0);
	assert.match(res.stderr, /SHA256SUMS/);
});

test("a release missing the license NOTICE blocks publication", async (t) => {
	const binaries = assetsFor(TAG);
	const fixture = await startFixture([
		...binaries,
		{ tag: TAG, name: "SHA256SUMS", body: sumsFile(binaries.map((b) => ({ name: b.name, body: b.body }))) },
	]);
	t.after(() => fixture.close());

	const res = await runNode(CHECK, [], { env: { SLIVINGDOC_RELEASE_BASE: fixture.base } });
	assert.notEqual(res.code, 0);
	assert.match(res.stderr, /NOTICE/);
});

test("a version whose release tag does not exist blocks publication", async (t) => {
	// The release exists only under a different tag: the package version and
	// the release tag have diverged, so publication must fail.
	const otherTag = "v0.1.0";
	const binaries = assetsFor(otherTag);
	const fixture = await startFixture([
		...binaries,
		{ tag: otherTag, name: "SHA256SUMS", body: sumsFile(binaries.map((b) => ({ name: b.name, body: b.body }))) },
		{ tag: otherTag, name: "NOTICE", body: "third-party notices" },
	]);
	t.after(() => fixture.close());

	const res = await runNode(CHECK, [], { env: { SLIVINGDOC_RELEASE_BASE: fixture.base } });
	assert.notEqual(res.code, 0);
	assert.match(res.stderr, new RegExp(`release ${TAG} is incomplete`));
});
