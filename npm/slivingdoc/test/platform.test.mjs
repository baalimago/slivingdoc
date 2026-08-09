// test/platform.test.mjs — the supported and unsupported target mapping.
import test from "node:test";
import assert from "node:assert/strict";
import { artifactFor, assetName, SUPPORTED_TARGETS, UnsupportedPlatformError } from "../lib/platform.mjs";

test("maps every supported platform to the release grammar", () => {
	const cases = [
		["linux", "x64", "linux", "amd64", false],
		["linux", "arm64", "linux", "arm64", false],
		["darwin", "x64", "darwin", "amd64", false],
		["darwin", "arm64", "darwin", "arm64", false],
		["win32", "x64", "windows", "amd64", true],
	];
	for (const [platform, arch, os, targetArch, exe] of cases) {
		const spec = artifactFor(platform, arch);
		assert.deepEqual(spec, { os, arch: targetArch, exe });
		assert.equal(assetName("0.1.0", spec), `slivingdoc-v0.1.0-${os}-${targetArch}${exe ? ".exe" : ""}`);
	}
});

test("the supported matrix is exactly the architecture section 21 targets", () => {
	assert.deepEqual(
		SUPPORTED_TARGETS,
		[
			{ os: "linux", arch: "amd64" },
			{ os: "linux", arch: "arm64" },
			{ os: "darwin", arch: "amd64" },
			{ os: "darwin", arch: "arm64" },
			{ os: "windows", arch: "amd64" },
		],
	);
});

test("rejects unsupported platforms before any download", () => {
	const cases = [
		["freebsd", "x64"],
		["linux", "ia32"],
		["linux", "arm"],
		["darwin", "ia32"],
		["win32", "arm64"], // windows arm64 is deferred
		["win32", "ia32"],
		["android", "arm64"],
	];
	for (const [platform, arch] of cases) {
		assert.throws(
			() => artifactFor(platform, arch),
			(err) => {
				assert.ok(err instanceof UnsupportedPlatformError);
				assert.match(err.message, /slivingdoc is not available for/);
				assert.match(err.message, new RegExp(platform));
				assert.match(err.message, /linux\/amd64/);
				return true;
			},
			`${platform}/${arch} must be rejected`,
		);
	}
});
